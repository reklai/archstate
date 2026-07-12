package archstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapDryRunReportsPackagesConfigsAndAURHelper(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	if err := os.MkdirAll(filepath.Join(env.repo, "config", "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := env.run("bootstrap", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	for _, want := range []string{
		"native install: neovim",
		"AUR install: paru-bin",
		"AUR helper: paru",
		"link " + filepath.Join(env.home, ".config", "nvim") + " -> " + filepath.Join(env.repo, "config", "nvim"),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("preview output missing %q:\n%s", want, out)
		}
	}
}

func TestBootstrapNoAURHelperReportsNextCommands(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader+"some-aur=desc\n")
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader)

	if err := env.run("bootstrap", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	for _, want := range []string{
		"AUR helper error: AUR packages are tracked, but neither paru nor yay is installed.",
		"archstate apply --aur-helper paru",
		"archstate apply --aur-helper yay",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("preview output missing %q:\n%s", want, out)
		}
	}

	err := env.run("bootstrap")
	if err == nil {
		t.Fatal("expected bootstrap to fail without AUR helper")
	}
	for _, want := range []string{
		"AUR packages are tracked, but neither paru nor yay is installed.",
		"archstate apply --aur-helper paru",
		"archstate apply --aur-helper yay",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("bootstrap error missing %q:\n%v", want, err)
		}
	}
}

func TestBootstrapAURHelperFlagBootstrapsMissingHelperAndContinues(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	logPath := filepath.Join(env.root, "aur-helper.log")
	env.r.Env = append(env.r.Env, "ARCHSTATE_LOG="+logPath, "ARCHSTATE_FAKE_BIN="+env.bin)
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
	writeExecutable(t, filepath.Join(env.bin, "sudo"), "echo \"sudo $*\" >> \"$ARCHSTATE_LOG\"\n")
	writeExecutable(t, filepath.Join(env.bin, "git"), `
echo "git $*" >> "$ARCHSTATE_LOG"
if [ "$1" = clone ]; then
  /bin/mkdir -p "$3"
fi
`)
	writeExecutable(t, filepath.Join(env.bin, "makepkg"), `
echo "makepkg $*" >> "$ARCHSTATE_LOG"
/bin/cat > "$ARCHSTATE_FAKE_BIN/paru" <<'HELPER'
#!/bin/sh
echo "paru $*" >> "$ARCHSTATE_LOG"
HELPER
/bin/chmod +x "$ARCHSTATE_FAKE_BIN/paru"
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader)
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader+"some-aur=desc\n")
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader)

	if err := env.run("bootstrap", "--aur-helper", "paru"); err != nil {
		t.Fatal(err)
	}
	log := readFile(t, logPath)
	for _, want := range []string{
		"sudo pacman -S --needed git base-devel",
		"git clone https://aur.archlinux.org/paru-bin.git",
		"makepkg -si",
		"paru -S --needed some-aur",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("helper bootstrap log missing %q:\n%s", want, log)
		}
	}
}

func TestBootstrapUsesAURHelperFromFallbackDir(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	fallbackBin := filepath.Join(env.root, "usr-bin")
	if err := os.MkdirAll(fallbackBin, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(env.root, "fallback.log")
	env.r.Env = append(env.r.Env, "ARCHSTATE_LOG="+logPath, "ARCHSTATE_AUR_HELPER_FALLBACK_DIR="+fallbackBin)
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
	writeExecutable(t, filepath.Join(fallbackBin, "paru"), "echo \"fallback-paru $*\" >> \"$ARCHSTATE_LOG\"\n")
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader)
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader+"some-aur=desc\n")
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader)

	if err := env.run("bootstrap"); err != nil {
		t.Fatal(err)
	}
	log := readFile(t, logPath)
	if !strings.Contains(log, "fallback-paru -S --needed some-aur") {
		t.Fatalf("fallback helper was not used:\n%s", log)
	}
}

func TestBootstrapAURHelperUsesFallbackDirAfterBuild(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	fallbackBin := filepath.Join(env.root, "usr-bin")
	if err := os.MkdirAll(fallbackBin, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(env.root, "aur-helper-fallback.log")
	env.r.Env = append(env.r.Env, "ARCHSTATE_LOG="+logPath, "ARCHSTATE_FAKE_BIN="+env.bin, "ARCHSTATE_AUR_HELPER_FALLBACK_DIR="+fallbackBin)
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
	writeExecutable(t, filepath.Join(env.bin, "sudo"), "echo \"sudo $*\" >> \"$ARCHSTATE_LOG\"\n")
	writeExecutable(t, filepath.Join(env.bin, "git"), `
echo "git $*" >> "$ARCHSTATE_LOG"
if [ "$1" = clone ]; then
  /bin/mkdir -p "$3"
fi
`)
	writeExecutable(t, filepath.Join(env.bin, "makepkg"), `
echo "makepkg $*" >> "$ARCHSTATE_LOG"
/bin/cat > "$ARCHSTATE_AUR_HELPER_FALLBACK_DIR/paru" <<'HELPER'
#!/bin/sh
echo "fallback-paru $*" >> "$ARCHSTATE_LOG"
HELPER
/bin/chmod +x "$ARCHSTATE_AUR_HELPER_FALLBACK_DIR/paru"
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader)
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader+"some-aur=desc\n")
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader)

	if err := env.run("bootstrap", "--aur-helper", "paru"); err != nil {
		t.Fatal(err)
	}
	log := readFile(t, logPath)
	for _, want := range []string{
		"makepkg -si",
		"fallback-paru -S --needed some-aur",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("fallback bootstrap log missing %q:\n%s", want, log)
		}
	}
}

func TestBootstrapAURHelperFlagDryRunShowsHelperBootstrap(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader+"some-aur=desc\n")
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader)

	if err := env.run("bootstrap", "--dry-run", "--aur-helper", "yay"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	for _, want := range []string{
		"AUR helper: yay",
		"AUR helper bootstrap: yay-bin",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("preview output missing %q:\n%s", want, out)
		}
	}
}

func TestBootstrapRejectsUnsupportedAURHelperFlag(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	err := env.run("bootstrap", "--aur-helper", "pacaur")
	if err == nil {
		t.Fatal("expected unsupported helper to fail")
	}
	if !strings.Contains(err.Error(), `unsupported AUR helper "pacaur"; choose paru or yay`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBootstrapWithNoAURPackagesDoesNotRequireAURHelper(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader)

	if err := env.run("bootstrap"); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapLeavesNativeInstallWhenAURInstallFails(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	logPath := filepath.Join(env.root, "partial-install.log")
	env.r.Env = append(env.r.Env, "ARCHSTATE_LOG="+logPath)
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
	writeExecutable(t, filepath.Join(env.bin, "sudo"), "echo \"sudo $*\" >> \"$ARCHSTATE_LOG\"\n")
	writeExecutable(t, filepath.Join(env.bin, "paru"), `
echo "paru $*" >> "$ARCHSTATE_LOG"
echo "target not found: missing-aur" >&2
exit 1
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader+"missing-aur=desc\n")
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader)

	err := env.run("bootstrap")
	if err == nil {
		t.Fatal("expected AUR install failure")
	}
	if !strings.Contains(err.Error(), "paru -S --needed missing-aur failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	log := readFile(t, logPath)
	for _, want := range []string{
		"sudo pacman -S --needed git",
		"paru -S --needed missing-aur",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("install log missing %q:\n%s", want, log)
		}
	}
}

func TestBootstrapAURHelperFlagDoesNotBypassManagedConflictSafety(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	logPath := filepath.Join(env.root, "should-not-run.log")
	env.r.Env = append(env.r.Env, "ARCHSTATE_LOG="+logPath)
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
	writeExecutable(t, filepath.Join(env.bin, "sudo"), "echo should-not-run >> \"$ARCHSTATE_LOG\"\n")
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader)
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader+"some-aur=desc\n")
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	if err := os.MkdirAll(filepath.Join(env.repo, "config", "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.home, ".config", "nvim"), "local config\n")

	err := env.run("bootstrap", "--aur-helper", "paru")
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "unmanaged conflict") {
		t.Fatalf("expected config conflict error, got %v", err)
	}
	if _, statErr := os.Stat(logPath); !os.IsNotExist(statErr) {
		t.Fatalf("AUR helper bootstrap ran before conflict validation")
	}
}

func TestBootstrapFailsConflictBeforeInstallingPackages(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	if err := os.MkdirAll(filepath.Join(env.repo, "config", "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.home, ".config", "nvim"), "local config\n")

	err := env.run("bootstrap")
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "unmanaged conflict") {
		t.Fatalf("expected conflict error, got %v", err)
	}
	if !strings.Contains(err.Error(), "use --adopt to save the current config into Archstate, or --restore to install the tracked copy over it") {
		t.Fatalf("expected conflict policy guidance, got %v", err)
	}
	if _, statErr := os.Stat(logPath); !os.IsNotExist(statErr) {
		t.Fatalf("package install ran before conflict validation")
	}
}

func TestBootstrapAdoptReplacesExistingRepoTargetWithLocalConfig(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	repoTarget := filepath.Join(env.repo, "config", "nvim")
	writeFile(t, filepath.Join(repoTarget, "old.lua"), "old repo config\n")
	local := filepath.Join(env.home, ".config", "nvim")
	writeFile(t, filepath.Join(local, "init.lua"), "local config wins\n")

	if err := env.run("bootstrap", "--adopt"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(repoTarget, "init.lua")); got != "local config wins\n" {
		t.Fatalf("repo target did not get local config: %q", got)
	}
	if _, err := os.Stat(filepath.Join(repoTarget, "old.lua")); !os.IsNotExist(err) {
		t.Fatalf("old repo target content was not replaced")
	}
	target, err := os.Readlink(local)
	if err != nil {
		t.Fatal(err)
	}
	if target != repoTarget {
		t.Fatalf("wrong symlink target: %s", target)
	}
}

func TestBootstrapAdoptWorksWhenRepoTargetIsMissing(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"mimeapps.list=mimeapps.list\n")
	local := filepath.Join(env.home, ".config", "mimeapps.list")
	writeFile(t, local, "local file config\n")
	repoTarget := filepath.Join(env.repo, "config", "mimeapps.list")

	if err := env.run("bootstrap", "--adopt"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, repoTarget); got != "local file config\n" {
		t.Fatalf("repo target did not get local file config: %q", got)
	}
	target, err := os.Readlink(local)
	if err != nil {
		t.Fatal(err)
	}
	if target != repoTarget {
		t.Fatalf("wrong symlink target: %s", target)
	}
}

func TestBootstrapRestoreConflict(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	if err := os.MkdirAll(filepath.Join(env.repo, "config", "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.home, ".config", "nvim"), "local config\n")

	if err := env.run("bootstrap", "--restore"); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(env.home, ".config", "nvim")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if target != filepath.Join(env.repo, "config", "nvim") {
		t.Fatalf("wrong symlink target: %s", target)
	}
}

func TestBootstrapRestoreFailsWhenRepoTargetIsMissing(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	local := filepath.Join(env.home, ".config", "nvim")
	writeFile(t, local, "local config\n")

	err := env.run("bootstrap", "--restore")
	if err == nil {
		t.Fatal("expected restore to fail without repo target")
	}
	if !strings.Contains(err.Error(), "cannot restore") || !strings.Contains(err.Error(), "no tracked copy exists") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readFile(t, local); got != "local config\n" {
		t.Fatalf("local config should remain untouched, got %q", got)
	}
}

func TestBootstrapDryRunReflectsConflictPolicy(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	repoTarget := filepath.Join(env.repo, "config", "nvim")
	if err := os.MkdirAll(repoTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(env.home, ".config", "nvim")
	writeFile(t, filepath.Join(local, "init.lua"), "local config\n")

	if err := env.run("bootstrap", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.stdout.String(), "conflict "+local+": use --adopt to save the current config into Archstate, or --restore to install the tracked copy over it") {
		t.Fatalf("preview did not show conflict policy:\n%s", env.stdout.String())
	}

	if err := env.run("bootstrap", "--dry-run", "--adopt"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.stdout.String(), "adopt "+local+" -> "+repoTarget+" (replacing tracked copy)") {
		t.Fatalf("preview did not show adopt action with replacement note:\n%s", env.stdout.String())
	}

	if err := env.run("bootstrap", "--dry-run", "--restore"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.stdout.String(), "restore "+repoTarget+" -> "+local) {
		t.Fatalf("preview did not show restore action:\n%s", env.stdout.String())
	}
}

func TestBootstrapAdoptRejectsForeignLocalSymlink(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
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

	err := env.run("bootstrap", "--adopt")
	if err == nil {
		t.Fatal("expected bootstrap adopt to reject foreign symlink")
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
}

func TestBootstrapRestoreReplacesForeignLocalSymlink(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	repoTarget := filepath.Join(env.repo, "config", "nvim")
	writeFile(t, filepath.Join(repoTarget, "init.lua"), "tracked config\n")
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

	if err := env.run("bootstrap", "--restore"); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(local)
	if err != nil {
		t.Fatal(err)
	}
	if target != repoTarget {
		t.Fatalf("wrong symlink target: %s", target)
	}
}

func TestBootstrapDotFilesAppliesSymlinksWithoutTouchingPacman(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	// pacman exits non-zero on any call, so a passing run proves --dotfiles
	// never queried or installed packages (needs no pacman, no sudo).
	writeFakePacman(t, env.bin, `echo "pacman must not run in --dotfiles mode: $*" >&2; exit 2`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"ripgrep=search tool\n")
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	repoTarget := filepath.Join(env.repo, "config", "nvim")
	writeFile(t, filepath.Join(repoTarget, "init.lua"), "tracked\n")

	if err := env.run("bootstrap", "--dotfiles"); err != nil {
		t.Fatal(err)
	}
	if !isCorrectSymlink(filepath.Join(env.home, ".config", "nvim"), repoTarget) {
		t.Fatalf("--dotfiles did not create the managed config symlink")
	}
}

func TestBootstrapDotFilesRejectsAURHelper(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	err := env.run("bootstrap", "--dotfiles", "--aur-helper", "paru")
	if err == nil {
		t.Fatal("expected --dotfiles with --aur-helper to fail")
	}
	if !strings.Contains(err.Error(), "--dotfiles skips packages, so --aur-helper has no effect") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBootstrapDotFilesDryRunSkipsPackagePlan(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `echo "pacman must not run in --dotfiles dry-run: $*" >&2; exit 2`)
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	repoTarget := filepath.Join(env.repo, "config", "nvim")
	writeFile(t, filepath.Join(repoTarget, "init.lua"), "tracked\n")

	if err := env.run("bootstrap", "--dotfiles", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	if !strings.Contains(out, "skipped (--dotfiles)") {
		t.Fatalf("dry-run did not note skipped packages:\n%s", out)
	}
	if !strings.Contains(out, "link "+filepath.Join(env.home, ".config", "nvim")) {
		t.Fatalf("dry-run did not show the config symlink plan:\n%s", out)
	}
	if strings.Contains(out, "native install:") {
		t.Fatalf("dry-run should not show a package plan in --dotfiles mode:\n%s", out)
	}
}

func TestBootstrapPackagesInstallsPackagesAndSkipsConflictingFiles(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	logPath := filepath.Join(env.root, "packages-only.log")
	env.r.Env = append(env.r.Env, "ARCHSTATE_LOG="+logPath)
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
	writeExecutable(t, filepath.Join(env.bin, "sudo"), "echo \"sudo $*\" >> \"$ARCHSTATE_LOG\"\n")
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"neovim=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader)
	// A config conflict that would stop a plain bootstrap before any install.
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	if err := os.MkdirAll(filepath.Join(env.repo, "config", "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(env.home, ".config", "nvim")
	writeFile(t, local, "local config\n")

	if err := env.run("bootstrap", "--packages"); err != nil {
		t.Fatalf("--packages should install packages despite the file conflict: %v", err)
	}
	if log := readFile(t, logPath); !strings.Contains(log, "sudo pacman -S --needed neovim") {
		t.Fatalf("packages were not installed:\n%s", log)
	}
	info, err := os.Lstat(local)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("--packages should leave config/home entries untouched")
	}
}

func TestBootstrapPackagesDryRunSkipsManagedPlan(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"neovim=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader)
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	if err := os.MkdirAll(filepath.Join(env.repo, "config", "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.home, ".config", "nvim"), "local config\n")

	if err := env.run("bootstrap", "--packages", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	if !strings.Contains(out, "native install: neovim") {
		t.Fatalf("dry-run did not show the package plan:\n%s", out)
	}
	if !strings.Contains(out, "skipped (--packages)") {
		t.Fatalf("dry-run did not note skipped config/home:\n%s", out)
	}
	if strings.Contains(out, "conflict") {
		t.Fatalf("--packages dry-run should not surface file conflicts:\n%s", out)
	}
}

func TestBootstrapPackagesRejectsConflictingFlags(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"bootstrap", "--packages", "--dotfiles"}, "mutually exclusive"},
		{[]string{"bootstrap", "--packages", "--adopt"}, "have no effect"},
		{[]string{"bootstrap", "--packages", "--restore"}, "have no effect"},
	}
	for _, tc := range cases {
		err := env.run(tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("run(%v): want error containing %q, got %v", tc.args, tc.want, err)
		}
	}
}

func TestBootstrapOverwriteFlagRenamedToRestore(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	for _, arg := range []string{"--overwrite", "--overwrite=true"} {
		err := env.run("bootstrap", arg)
		if err == nil || !strings.Contains(err.Error(), "the --overwrite flag was renamed to --restore") {
			t.Fatalf("run(bootstrap %s): want rename guidance, got %v", arg, err)
		}
	}
}
