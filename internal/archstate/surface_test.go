package archstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupCleanMachine(t *testing.T, env *testEnv) {
	t.Helper()
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen) printf 'git\n' ;;
  -Qqem) printf '' ;;
  -Qq) printf 'git\n' ;;
  *) echo "unexpected pacman args: $*" >&2; exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(env.bin, "sudo"), `echo "sudo $*" >&2; exit 0`)
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
}

func TestCheckStatusSubsetOmitsDoctor(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	if err := env.run("check", "--status"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	for _, want := range []string{
		"native missing: none",
		"ok nvim",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("check --status missing %q:\n%s", want, out)
		}
	}
	// --status is drift-only; no doctor section.
	if strings.Contains(out, "OK repo:") {
		t.Fatalf("check --status should omit doctor output:\n%s", out)
	}
}

func TestCheckGateCompactOutput(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	if err := env.run("check", "--gate"); err != nil {
		t.Fatalf("check --gate: %v\n%s", err, env.stdout.String())
	}
	out := env.stdout.String()
	if !strings.Contains(out, "check: ok") {
		t.Fatalf("expected check: ok:\n%s", out)
	}
	// --gate is compact; not the full check listing.
	if strings.Contains(out, "Package status:") {
		t.Fatalf("check --gate should not print status listing:\n%s", out)
	}
	if strings.Contains(out, "OK repo:") {
		t.Fatalf("check --gate should not print doctor listing:\n%s", out)
	}
}

func TestCheckDoctorSubsetOmitsStatus(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	if err := env.run("check", "--doctor"); err != nil {
		t.Fatalf("check --doctor: %v\n%s", err, env.stdout.String())
	}
	out := env.stdout.String()
	if !strings.Contains(out, "OK repo:") {
		t.Fatalf("check --doctor missing health report:\n%s", out)
	}
	if strings.Contains(out, "Package status:") {
		t.Fatalf("check --doctor should not print status listing:\n%s", out)
	}
}

func TestCheckCoverageSubsetOmitsStatus(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	if err := env.run("check", "--coverage"); err != nil {
		t.Fatalf("check --coverage: %v\n%s", err, env.stdout.String())
	}
	out := env.stdout.String()
	if !strings.Contains(out, "Config coverage") {
		t.Fatalf("check --coverage missing report:\n%s", out)
	}
	if strings.Contains(out, "Package status:") {
		t.Fatalf("check --coverage should be coverage-only:\n%s", out)
	}
}

func TestCheckDispatchDefault(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	if err := env.run("check"); err != nil {
		t.Fatalf("check: %v\n%s", err, env.stdout.String())
	}
	out := env.stdout.String()
	for _, want := range []string{
		"Package status:",
		"ok nvim",
		"OK repo:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("check default missing %q:\n%s", want, out)
		}
	}
}

func TestCheckDoesNotFailOnDriftWithoutExit(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	// Missing tracked package = drift.
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=desc\nneovim=desc\n")
	if err := env.run("check"); err != nil {
		t.Fatalf("default check should not fail on drift: %v\n%s", err, env.stdout.String())
	}
	if !strings.Contains(env.stdout.String(), "native missing: neovim") {
		t.Fatalf("check should still report missing packages:\n%s", env.stdout.String())
	}
}

func TestCheckDoesNotFailWhenDoctorReportsError(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	// Break managed symlink so doctor prints ERROR.
	local := filepath.Join(env.home, ".config", "nvim")
	if err := os.Remove(local); err != nil {
		t.Fatal(err)
	}
	if err := env.run("check"); err != nil {
		t.Fatalf("default check should not fail when doctor ERROR lines print: %v\n%s", err, env.stdout.String())
	}
	out := env.stdout.String()
	if !strings.Contains(out, "ERROR config nvim:") {
		t.Fatalf("check should include doctor ERROR:\n%s", out)
	}
}

func TestCheckRunsDoctorWhenPacmanMissing(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	// Empty PATH: no pacman. Status cannot be collected, but doctor should still run.
	env.r.Env = []string{"HOME=" + env.home, "PATH=" + filepath.Join(env.root, "empty-bin")}
	if err := os.MkdirAll(filepath.Join(env.root, "empty-bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := env.run("check")
	if err == nil {
		t.Fatal("check should still fail when package status is unavailable")
	}
	out := env.stdout.String()
	if !strings.Contains(out, "Package status: unavailable") {
		t.Fatalf("check should report unavailable status:\n%s", out)
	}
	if !strings.Contains(out, "ERROR pacman command:") && !strings.Contains(out, "OK repo:") {
		// Doctor must have printed something useful even if path lookup wording varies.
		if !strings.Contains(out, "ERROR") {
			t.Fatalf("check should still print doctor health when drift fails:\n%s", out)
		}
	}
	if !strings.Contains(out, "ERROR pacman command:") {
		t.Fatalf("doctor section should report missing pacman:\n%s", out)
	}
}

func TestCheckExit(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=desc\nneovim=desc\n")
	err := env.run("check", "--exit")
	if err == nil {
		t.Fatal("expected check --exit to fail on missing package")
	}
	if !strings.Contains(err.Error(), "check found drift") {
		t.Fatalf("unexpected error: %v", err)
	}
	out := env.stdout.String()
	for _, want := range []string{
		"Package status:",
		"OK repo:",
		"check: failed",
		"native missing: neovim",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("check --exit missing %q:\n%s", want, out)
		}
	}
	// Primary check --exit should not use verify: messaging.
	if strings.Contains(out, "verify: failed") || strings.Contains(out, "verify: ok") {
		t.Fatalf("check --exit should use check: messaging, not verify:\n%s", out)
	}
}

func TestCheckExitCleanSuccess(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	if err := env.run("check", "--exit"); err != nil {
		t.Fatalf("check --exit on clean machine: %v\n%s", err, env.stdout.String())
	}
	if !strings.Contains(env.stdout.String(), "check: ok") {
		t.Fatalf("expected check: ok:\n%s", env.stdout.String())
	}
}

func TestCheckExitStrictPackages(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen) printf 'git\nripgrep\n' ;;
  -Qqem) printf '' ;;
  -Qq) printf 'git\nripgrep\n' ;;
  *) echo "unexpected pacman args: $*" >&2; exit 2 ;;
esac
`)
	err := env.run("check", "--exit", "--strict-packages")
	if err == nil {
		t.Fatal("expected strict check --exit to fail on untracked")
	}
	if !strings.Contains(env.stdout.String(), "native untracked: ripgrep") {
		t.Fatalf("expected untracked failure:\n%s", env.stdout.String())
	}
}

func TestCheckExitPackagesOnly(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	// Config conflict would fail full check --exit.
	local := filepath.Join(env.home, ".config", "nvim")
	if err := os.Remove(local); err != nil {
		t.Fatal(err)
	}
	writeFile(t, local, "unmanaged\n")

	if err := env.run("check", "--exit", "--packages-only"); err != nil {
		t.Fatalf("packages-only should ignore config conflict: %v\n%s", err, env.stdout.String())
	}
}

func TestCheckRejectsMutuallyExclusiveScopeFlags(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	err := env.run("check", "--packages-only", "--dotfiles-only")
	if err == nil {
		t.Fatal("expected mutual exclusion error")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckRejectsUnknownFlag(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	err := env.run("check", "--nope")
	if err == nil {
		t.Fatal("expected unknown flag error")
	}
	if !strings.Contains(err.Error(), "usage: archstate check") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyDispatchDryRun(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	if err := env.run("apply", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	for _, want := range []string{
		"Package plan:",
		"native install: none",
		"AUR install: none",
		"Config plan:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("apply --dry-run missing %q:\n%s", want, out)
		}
	}
}

func TestTrackConfigDispatch(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	if err := env.run("track", "config", "list"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.stdout.String(), "nvim") {
		t.Fatalf("track config list missing nvim:\n%s", env.stdout.String())
	}
}

func TestTrackHomeDispatch(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	writeFile(t, filepath.Join(env.home, ".zshrc"), "export EDITOR=nvim\n")
	if err := env.run("track", "home", "add", ".zshrc"); err != nil {
		t.Fatal(err)
	}
	if err := env.run("track", "home", "list"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.stdout.String(), ".zshrc") {
		t.Fatalf("track home list missing .zshrc:\n%s", env.stdout.String())
	}
}

func TestTrackBareRequiresTerminal(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	err := env.run("track")
	if err == nil {
		t.Fatal("expected bare track to require interactive terminal")
	}
	if !strings.Contains(err.Error(), "archstate track requires an interactive terminal") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTrackUntrackRequiresTerminal(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	err := env.run("track", "untrack")
	if err == nil {
		t.Fatal("expected track untrack to require interactive terminal")
	}
	if !strings.Contains(err.Error(), "archstate track untrack requires an interactive terminal") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTrackConfigUsagePrefix(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	err := env.run("track", "config")
	if err == nil {
		t.Fatal("expected usage error")
	}
	if !strings.Contains(err.Error(), "usage: archstate track config") {
		t.Fatalf("track config usage should mention track prefix: %v", err)
	}
}
