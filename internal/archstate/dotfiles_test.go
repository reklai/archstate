package archstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDotAddAdoptsExistingConfig(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFile(t, filepath.Join(env.home, ".config", "nvim", "init.lua"), "vim.opt.number = true\n")

	if err := env.run("dot", "add", "nvim"); err != nil {
		t.Fatal(err)
	}
	repoTarget := filepath.Join(env.repo, "dotfiles", "nvim")
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
	conf := readFile(t, filepath.Join(env.repo, "dotfiles.conf"))
	if !strings.Contains(conf, "nvim=nvim\n") {
		t.Fatalf("dotfiles.conf missing mapping:\n%s", conf)
	}
}

func TestDotAddReportsNothingWhenNoSourceExists(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	if err := env.run("dot", "add", "nvim"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.stdout.String(), "nothing to add for nvim") {
		t.Fatalf("unexpected output: %s", env.stdout.String())
	}
	conf := readFile(t, filepath.Join(env.repo, "dotfiles.conf"))
	if strings.Contains(conf, "nvim=nvim") {
		t.Fatalf("unexpected mapping:\n%s", conf)
	}
}

func TestDotRemoveRemovesManagedSymlinkAndPreservesRepoData(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	repoTarget := filepath.Join(env.repo, "dotfiles", "nvim")
	if err := os.MkdirAll(repoTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repoTarget, "init.lua"), "vim.opt.number = true\n")
	writeFile(t, filepath.Join(env.repo, "dotfiles.conf"), generatedHeader+"nvim=nvim\n")
	local := filepath.Join(env.home, ".config", "nvim")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repoTarget, local); err != nil {
		t.Fatal(err)
	}

	if err := env.run("dot", "rm", "nvim"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(local); !os.IsNotExist(err) {
		t.Fatalf("expected managed symlink to be removed")
	}
	if _, err := os.Stat(filepath.Join(repoTarget, "init.lua")); err != nil {
		t.Fatalf("repo data was not preserved: %v", err)
	}
	conf := readFile(t, filepath.Join(env.repo, "dotfiles.conf"))
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
	repoTarget := filepath.Join(env.repo, "dotfiles", "nvim")
	if err := os.MkdirAll(repoTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.repo, "dotfiles.conf"), generatedHeader+"nvim=nvim\n")

	err := env.run("doctor")
	if err == nil {
		t.Fatal("expected doctor to fail")
	}
	out := env.stdout.String()
	if !strings.Contains(out, "ERROR dotfile health: managed symlink is missing") {
		t.Fatalf("unexpected doctor output:\n%s", out)
	}
}
