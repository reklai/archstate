package archstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusReportsPackageAndConfigDrift(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen)
    printf 'git\nripgrep\n'
    ;;
  -Qqem)
    printf 'visual-studio-code-bin\n'
    ;;
  *)
    echo "unexpected pacman args: $*" >&2
    exit 2
    ;;
esac
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=desc\nneovim=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader+"paru-bin=desc\n")
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"gtk=gtk\nmimeapps.list=mimeapps.list\nnvim=nvim\nshell=shell\n")
	if err := os.MkdirAll(filepath.Join(env.repo, "config", "mimeapps.list"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(env.repo, "config", "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(env.repo, "config", "gtk"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(env.home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(env.repo, "config", "nvim"), filepath.Join(env.home, ".config", "nvim")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.home, ".config", "gtk"), "unmanaged\n")

	if err := env.run("status"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	for _, want := range []string{
		"native missing: neovim",
		"native untracked: ripgrep",
		"AUR missing: paru-bin",
		"AUR untracked: visual-studio-code-bin",
		"conflict gtk: use --adopt to save the current config into Archstate, or --restore to install the tracked copy over it",
		"missing mimeapps.list: needs link",
		"ok nvim",
		`error shell: config "shell" is tracked but its saved copy is missing`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output missing %q:\n%s", want, out)
		}
	}
}

func TestStatusReportsCleanState(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen)
    printf 'git\n'
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
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader)
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	repoTarget := filepath.Join(env.repo, "config", "nvim")
	if err := os.MkdirAll(repoTarget, 0o755); err != nil {
		t.Fatal(err)
	}
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
	out := env.stdout.String()
	for _, want := range []string{
		"native missing: none",
		"native untracked: none",
		"AUR missing: none",
		"AUR untracked: none",
		"ok nvim",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output missing %q:\n%s", want, out)
		}
	}
}
