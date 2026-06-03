package archstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapPreviewReportsPackagesDotfilesAndAURHelper(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qq)
    printf 'git\n'
    ;;
  *)
    echo "unexpected pacman args: $*" >&2
    exit 2
    ;;
esac
`)
	writeExecutable(t, filepath.Join(env.bin, "paru"), "exit 0\n")
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=desc\nneovim=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader+"paru-bin=desc\n")
	writeFile(t, filepath.Join(env.repo, "dotfiles.conf"), generatedHeader+"nvim=nvim\n")
	if err := os.MkdirAll(filepath.Join(env.repo, "dotfiles", "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := env.run("bootstrap", "--preview"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	for _, want := range []string{
		"native install: neovim",
		"AUR install: paru-bin",
		"AUR helper: paru",
		"link " + filepath.Join(env.home, ".config", "nvim") + " -> " + filepath.Join(env.repo, "dotfiles", "nvim"),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("preview output missing %q:\n%s", want, out)
		}
	}
}

func TestBootstrapFailsNonInteractiveConflictBeforeInstallingPackages(t *testing.T) {
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
	writeExecutable(t, filepath.Join(env.bin, "sudo"), "echo should-not-install >> '$ARCHSTATE_LOG'\n")
	logPath := filepath.Join(env.root, "install.log")
	env.r.Env = append(env.r.Env, "ARCHSTATE_LOG="+logPath)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"neovim=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader)
	writeFile(t, filepath.Join(env.repo, "dotfiles.conf"), generatedHeader+"nvim=nvim\n")
	if err := os.MkdirAll(filepath.Join(env.repo, "dotfiles", "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.home, ".config", "nvim"), "local config\n")

	err := env.run("bootstrap")
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "unmanaged dotfile conflict") {
		t.Fatalf("expected conflict error, got %v", err)
	}
	if _, statErr := os.Stat(logPath); !os.IsNotExist(statErr) {
		t.Fatalf("package install ran before conflict validation")
	}
}

func TestBootstrapOverwriteConflict(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader)
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader)
	writeFile(t, filepath.Join(env.repo, "dotfiles.conf"), generatedHeader+"nvim=nvim\n")
	if err := os.MkdirAll(filepath.Join(env.repo, "dotfiles", "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.home, ".config", "nvim"), "local config\n")

	if err := env.run("bootstrap", "--overwrite"); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(env.home, ".config", "nvim")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if target != filepath.Join(env.repo, "dotfiles", "nvim") {
		t.Fatalf("wrong symlink target: %s", target)
	}
}
