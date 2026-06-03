package archstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigAddAdoptsExistingConfig(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFile(t, filepath.Join(env.home, ".config", "nvim", "init.lua"), "vim.opt.number = true\n")

	if err := env.run("config", "add", "nvim"); err != nil {
		t.Fatal(err)
	}
	repoTarget := filepath.Join(env.repo, "config", "nvim")
	if _, err := os.Stat(filepath.Join(repoTarget, "init.lua")); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(env.home, ".config", "nvim")
	link, err := os.Readlink(local)
	if err != nil {
		t.Fatal(err)
	}
	if link != repoTarget {
		t.Fatalf("wrong symlink target: %s", link)
	}
	conf := readFile(t, filepath.Join(env.repo, "config.conf"))
	if !strings.Contains(conf, "nvim=nvim\n") {
		t.Fatalf("config.conf missing mapping:\n%s", conf)
	}
}

func TestConfigAddReportsNothingWhenNoSourceExists(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	if err := env.run("config", "add", "nvim"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.stdout.String(), "nothing to add for nvim") {
		t.Fatalf("unexpected output: %s", env.stdout.String())
	}
	conf := readFile(t, filepath.Join(env.repo, "config.conf"))
	if strings.Contains(conf, "nvim=nvim") {
		t.Fatalf("unexpected mapping:\n%s", conf)
	}
}

func TestConfigRemoveRestoresLocalConfigAndRemovesRepoData(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	repoTarget := filepath.Join(env.repo, "config", "nvim")
	if err := os.MkdirAll(repoTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repoTarget, "init.lua"), "vim.opt.number = true\n")
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	local := filepath.Join(env.home, ".config", "nvim")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repoTarget, local); err != nil {
		t.Fatal(err)
	}

	if err := env.run("config", "rm", "nvim"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(local)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected local config to be restored as a real directory")
	}
	if got := readFile(t, filepath.Join(local, "init.lua")); got != "vim.opt.number = true\n" {
		t.Fatalf("local config was not restored: %q", got)
	}
	if _, err := os.Lstat(repoTarget); !os.IsNotExist(err) {
		t.Fatalf("expected repo data to be removed")
	}
	conf := readFile(t, filepath.Join(env.repo, "config.conf"))
	if strings.Contains(conf, "nvim=nvim") {
		t.Fatalf("mapping was not removed:\n%s", conf)
	}
}

func TestDoctorReportsMissingManagedSymlink(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qq)
    printf ''
    ;;
  *)
    echo "unexpected pacman args: $*" >&2
    exit 2
    ;;
esac
`)
	writeExecutable(t, filepath.Join(env.bin, "sudo"), "exit 0\n")
	repoTarget := filepath.Join(env.repo, "config", "nvim")
	if err := os.MkdirAll(repoTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")

	err := env.run("doctor")
	if err == nil {
		t.Fatal("expected doctor to fail")
	}
	out := env.stdout.String()
	for _, want := range []string{
		"ERROR config nvim: managed symlink is missing",
		"local: " + filepath.Join(env.home, ".config", "nvim"),
		"tracked: " + repoTarget,
		"preview: archstate bootstrap --preview",
		"fix: archstate bootstrap",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	}
}

func TestDoctorReportsAURHelperFixes(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qq)
    printf ''
    ;;
  -Qqen)
    printf ''
    ;;
  -Qqem)
    printf ''
    ;;
  *)
    echo "unexpected pacman args: $*" >&2
    exit 2
    ;;
esac
`)
	writeExecutable(t, filepath.Join(env.bin, "sudo"), "exit 0\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader+"some-aur=desc\n")

	err := env.run("doctor")
	if err == nil {
		t.Fatal("expected doctor to fail")
	}
	out := env.stdout.String()
	for _, want := range []string{
		"ERROR AUR helper:",
		"fix: archstate bootstrap --aur-helper paru",
		"fix: archstate bootstrap --aur-helper yay",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	}
}

func TestDoctorWarnsPackageDriftWithoutFailing(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qq)
    printf 'git\nripgrep\n'
    ;;
  -Qqen)
    printf 'git\nripgrep\n'
    ;;
  -Qqem)
    printf ''
    ;;
  *)
    echo "unexpected pacman args: $*" >&2
    exit 2
    ;;
esac
`)
	writeExecutable(t, filepath.Join(env.bin, "sudo"), "exit 0\n")
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=desc\n")

	if err := env.run("doctor"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	for _, want := range []string{
		"WARN package drift: 1 explicit packages are not tracked",
		"inspect: archstate status",
		"accept current machine: archstate sync",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ERROR") {
		t.Fatalf("unexpected doctor output:\n%s", out)
	}
}
