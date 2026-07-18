package archstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckGateOkWhenMachineMatches(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen) printf 'git\n' ;;
  -Qqem) printf '' ;;
  *) echo "unexpected pacman args: $*" >&2; exit 2 ;;
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

	if err := env.run("check", "--gate"); err != nil {
		t.Fatalf("check --gate: %v\n%s", err, env.stdout.String())
	}
	if !strings.Contains(env.stdout.String(), "check: ok") {
		t.Fatalf("expected ok:\n%s", env.stdout.String())
	}
}

func TestCheckGateFailsOnMissingPackagesAndConflicts(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen) printf 'git\nripgrep\n' ;;
  -Qqem) printf '' ;;
  *) echo "unexpected pacman args: $*" >&2; exit 2 ;;
esac
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=desc\nneovim=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader)
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"gtk=gtk\n")
	if err := os.MkdirAll(filepath.Join(env.repo, "config", "gtk"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(env.home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.home, ".config", "gtk"), "unmanaged\n")

	err := env.run("check", "--gate")
	if err == nil {
		t.Fatal("expected check --gate failure")
	}
	if !strings.Contains(err.Error(), "check found drift") {
		t.Fatalf("unexpected error: %v", err)
	}
	out := env.stdout.String()
	for _, want := range []string{
		"check: failed",
		"native missing: neovim",
		"config conflict: gtk",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("check --gate output missing %q:\n%s", want, out)
		}
	}
	// Untracked packages are not failures without --strict-packages.
	if strings.Contains(out, "untracked") {
		t.Fatalf("default check --gate should not fail on untracked:\n%s", out)
	}
}

func TestCheckGateStrictPackagesFailsOnUntracked(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen) printf 'git\nripgrep\n' ;;
  -Qqem) printf '' ;;
  *) echo "unexpected pacman args: $*" >&2; exit 2 ;;
esac
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader)

	err := env.run("check", "--gate", "--strict-packages")
	if err == nil {
		t.Fatal("expected strict check --gate failure")
	}
	if !strings.Contains(env.stdout.String(), "native untracked: ripgrep") {
		t.Fatalf("expected untracked failure:\n%s", env.stdout.String())
	}
}

func TestCheckGatePackagesOnlySkipsConfig(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen) printf 'git\n' ;;
  -Qqem) printf '' ;;
  *) echo "unexpected pacman args: $*" >&2; exit 2 ;;
esac
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader)
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"gtk=gtk\n")
	if err := os.MkdirAll(filepath.Join(env.repo, "config", "gtk"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(env.home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.home, ".config", "gtk"), "unmanaged\n")

	if err := env.run("check", "--gate", "--packages-only"); err != nil {
		t.Fatalf("packages-only should ignore config conflict: %v\n%s", err, env.stdout.String())
	}
}

func TestCheckGateDotfilesOnlySkipsPackages(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen) printf 'git\n' ;;
  -Qqem) printf '' ;;
  *) echo "unexpected pacman args: $*" >&2; exit 2 ;;
esac
`)
	// Tracked neovim is missing, but --dotfiles-only should ignore packages.
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"neovim=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader)
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader)

	if err := env.run("check", "--gate", "--dotfiles-only"); err != nil {
		t.Fatalf("dotfiles-only should ignore missing packages: %v\n%s", err, env.stdout.String())
	}
}

func TestCheckGateDotfilesOnlyDoesNotRequirePacman(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	// No pacman binary in PATH. --dotfiles-only must not touch package layer.
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"neovim=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader)
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader)

	if err := env.run("check", "--gate", "--dotfiles-only"); err != nil {
		t.Fatalf("dotfiles-only must succeed without pacman: %v\n%s", err, env.stdout.String())
	}
	if !strings.Contains(env.stdout.String(), "check: ok") {
		t.Fatalf("expected check: ok:\n%s", env.stdout.String())
	}
}

func TestCheckGatePackagesOnlyDoesNotRequireManagedState(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen) printf 'git\n' ;;
  -Qqem) printf '' ;;
  *) echo "unexpected pacman args: $*" >&2; exit 2 ;;
esac
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader)
	// Malformed config must not abort packages-only gate.
	writeFile(t, filepath.Join(env.repo, "config.conf"), "not=valid=managed\n")

	if err := env.run("check", "--gate", "--packages-only"); err != nil {
		t.Fatalf("packages-only must ignore broken config: %v\n%s", err, env.stdout.String())
	}
}

func TestCheckGateRemediationForUntrackedOnly(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen) printf 'git\nripgrep\n' ;;
  -Qqem) printf '' ;;
  *) echo "unexpected pacman args: $*" >&2; exit 2 ;;
esac
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader)

	err := env.run("check", "--gate", "--strict-packages", "--packages-only")
	if err == nil {
		t.Fatal("expected failure on untracked")
	}
	out := env.stdout.String()
	for _, want := range []string{
		"native untracked: ripgrep",
		"accept untracked packages: archstate sync",
		"or ignore: archstate ignore add <pkg>",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing remediation %q:\n%s", want, out)
		}
	}
	// apply --packages cannot clear untracked-only failures.
	if strings.Contains(out, "fix packages: archstate apply --packages") {
		t.Fatalf("should not suggest apply --packages for untracked-only:\n%s", out)
	}
	if strings.Contains(out, "fix files: archstate apply --dry-run") {
		t.Fatalf("should not suggest dry-run as a file fix for package-only failure:\n%s", out)
	}
}

func TestCheckGateRemediationForMissingPackagesAndManaged(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen) printf 'git\n' ;;
  -Qqem) printf '' ;;
  *) echo "unexpected pacman args: $*" >&2; exit 2 ;;
esac
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=desc\nneovim=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader)
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	if err := os.MkdirAll(filepath.Join(env.repo, "config", "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Missing local symlink → managed missing.

	err := env.run("check", "--gate")
	if err == nil {
		t.Fatal("expected check --gate failure")
	}
	out := env.stdout.String()
	for _, want := range []string{
		"fix packages: archstate apply --packages",
		"fix missing links: archstate apply --dotfiles",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing remediation %q:\n%s", want, out)
		}
	}
}

func TestCheckGateMutuallyExclusiveFlags(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	err := env.run("check", "--gate", "--packages-only", "--dotfiles-only")
	if err == nil {
		t.Fatal("expected mutual exclusion error")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("unexpected error: %v", err)
	}
}
