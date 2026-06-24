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

func TestConfigListShowsTrackedEntries(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim-local\nmimeapps.list=mimeapps.list\n")

	if err := env.run("config", "list"); err != nil {
		t.Fatal(err)
	}

	want := "Tracked config entries:\n" +
		"  mimeapps.list -> config/mimeapps.list\n" +
		"  nvim -> config/nvim-local\n"
	if env.stdout.String() != want {
		t.Fatalf("config list output mismatch\nwant:\n%s\ngot:\n%s", want, env.stdout.String())
	}
}

func TestConfigListShowsEmptyState(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	if err := env.run("config", "list"); err != nil {
		t.Fatal(err)
	}

	if got := env.stdout.String(); got != "no config entries tracked\n" {
		t.Fatalf("unexpected config list output: %q", got)
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

func TestConfigAddRejectsForeignLocalSymlink(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	foreignTarget := filepath.Join(env.root, "elsewhere", "nvim")
	if err := os.MkdirAll(foreignTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(env.home, ".config", "nvim")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreignTarget, local); err != nil {
		t.Fatal(err)
	}

	err := env.run("config", "add", "nvim")
	if err == nil {
		t.Fatal("expected foreign symlink adoption to fail")
	}
	if !strings.Contains(err.Error(), "is a symlink") {
		t.Fatalf("unexpected error: %v", err)
	}
	target, err := os.Readlink(local)
	if err != nil {
		t.Fatal(err)
	}
	if target != foreignTarget {
		t.Fatalf("foreign symlink was changed: %s", target)
	}
	if _, statErr := os.Lstat(filepath.Join(env.repo, "config", "nvim")); !os.IsNotExist(statErr) {
		t.Fatalf("repo target should not have been created")
	}
}

func TestConfigAddRejectsRepoTargetSymlink(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	trackedTarget := filepath.Join(env.root, "tracked-target")
	writeFile(t, trackedTarget, "target\n")
	repoTarget := filepath.Join(env.repo, "config", "nvim")
	if err := os.Symlink(trackedTarget, repoTarget); err != nil {
		t.Fatal(err)
	}

	err := env.run("config", "add", "nvim")
	if err == nil {
		t.Fatal("expected repo symlink target to fail")
	}
	if !strings.Contains(err.Error(), "must be a real file or dir, not a symlink") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigRemoveRejectsRepoTargetSymlinkBeforeSnapshot(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	env.r.Now = fixedTime(2026, 6, 4, 19, 0, 0)
	trackedTarget := filepath.Join(env.root, "tracked-target")
	writeFile(t, trackedTarget, "target\n")
	repoTarget := filepath.Join(env.repo, "config", "nvim")
	if err := os.Symlink(trackedTarget, repoTarget); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")

	err := env.run("config", "rm", "nvim")
	if err == nil {
		t.Fatal("expected repo symlink target to fail")
	}
	if !strings.Contains(err.Error(), "must be a real file or dir, not a symlink") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(env.repo, ".snapshots", "auto-2026-06-04_19-00-00")); !os.IsNotExist(statErr) {
		t.Fatalf("remove should fail before creating an auto snapshot")
	}
}

func TestStatusReportsBrokenManagedSymlink(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
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
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	repoTarget := filepath.Join(env.repo, "config", "nvim")
	local := filepath.Join(env.home, ".config", "nvim")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repoTarget, local); err != nil {
		t.Fatal(err)
	}

	if err := env.run("status"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.stdout.String(), `error nvim: config "nvim" is a managed symlink but its tracked copy is missing`) {
		t.Fatalf("status did not report broken managed symlink:\n%s", env.stdout.String())
	}
}

func TestConfigAddRollsBackAdoptionWhenStateWriteFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write through directory permissions used by this test")
	}
	env := newTestEnv(t)
	env.initRepo(t)
	if err := os.MkdirAll(filepath.Join(env.repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(env.bin, "git"), `
if [ "$1" = -C ] && [ "$3" = status ] && [ "$4" = --porcelain ]; then
  printf ''
  exit 0
fi
echo "unexpected git args: $*" >&2
exit 2
`)
	local := filepath.Join(env.home, ".config", "nvim")
	writeFile(t, filepath.Join(local, "init.lua"), "local config\n")
	if err := os.Chmod(env.repo, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(env.repo, 0o755)
	})

	err := env.run("config", "add", "nvim")
	if err == nil {
		t.Fatal("expected state write to fail")
	}
	info, statErr := os.Lstat(local)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("local config should have been restored as a real directory")
	}
	if got := readFile(t, filepath.Join(local, "init.lua")); got != "local config\n" {
		t.Fatalf("local config was not restored: %q", got)
	}
	if _, statErr := os.Lstat(filepath.Join(env.repo, "config", "nvim")); !os.IsNotExist(statErr) {
		t.Fatalf("repo target should have been rolled back")
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
		"dry-run: archstate bootstrap --dry-run",
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
