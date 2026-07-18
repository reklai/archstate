package archstate

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestIgnoreAddListRemove(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	if err := env.run("ignore", "add", "linux-zen", "nvidia"); err != nil {
		t.Fatal(err)
	}
	if err := env.run("ignore", "list"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	for _, want := range []string{"linux-zen", "nvidia"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list missing %q:\n%s", want, out)
		}
	}
	data := readFile(t, filepath.Join(env.repo, packagesIgnoreFile))
	if !strings.Contains(data, "linux-zen\n") || !strings.Contains(data, "nvidia\n") {
		t.Fatalf("packages.ignore content:\n%s", data)
	}

	if err := env.run("ignore", "rm", "nvidia"); err != nil {
		t.Fatal(err)
	}
	if err := env.run("ignore", "list"); err != nil {
		t.Fatal(err)
	}
	out = env.stdout.String()
	if !strings.Contains(out, "linux-zen") {
		t.Fatalf("list should still show linux-zen:\n%s", out)
	}
	if strings.Contains(out, "nvidia") {
		t.Fatalf("list should not show nvidia:\n%s", out)
	}
}

func TestSyncSkipsIgnoredPackages(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen) printf 'git\nlinux-zen\nripgrep\n' ;;
  -Qqem) printf '' ;;
  -Qi)
    for pkg in "$@"; do
      [ "$pkg" = "-Qi" ] && continue
      printf 'Name            : %s\nDescription     : desc for %s\n\n' "$pkg" "$pkg"
    done
    ;;
  *) echo "unexpected pacman args: $*" >&2; exit 2 ;;
esac
`)
	writeFile(t, filepath.Join(env.repo, packagesIgnoreFile), generatedHeader+"linux-zen\n")

	if err := env.run("sync"); err != nil {
		t.Fatal(err)
	}
	pacman := readFile(t, filepath.Join(env.repo, "pacman.conf"))
	if strings.Contains(pacman, "linux-zen") {
		t.Fatalf("sync should not track ignored package:\n%s", pacman)
	}
	if !strings.Contains(pacman, "git=") || !strings.Contains(pacman, "ripgrep=") {
		t.Fatalf("sync should track non-ignored packages:\n%s", pacman)
	}
}

func TestCheckIgnoresUntrackedIgnoredPackages(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen) printf 'git\nlinux-zen\n' ;;
  -Qqem) printf '' ;;
  *) echo "unexpected pacman args: $*" >&2; exit 2 ;;
esac
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader)
	writeFile(t, filepath.Join(env.repo, packagesIgnoreFile), generatedHeader+"linux-zen\n")

	if err := env.run("check", "--status"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(env.stdout.String(), "linux-zen") {
		t.Fatalf("check --status should not report ignored package as untracked:\n%s", env.stdout.String())
	}
	if err := env.run("check", "--gate", "--strict-packages"); err != nil {
		t.Fatalf("strict gate should pass when only ignored packages are extra: %v\n%s", err, env.stdout.String())
	}
}

func TestApplyDoesNotInstallIgnoredTrackedPackages(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qq) printf 'git\n' ;;
  *) echo "unexpected pacman args: $*" >&2; exit 2 ;;
esac
`)
	// Stale tracked entry for an ignored package must not be installed.
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=desc\nlinux-zen=desc\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader)
	writeFile(t, filepath.Join(env.repo, packagesIgnoreFile), generatedHeader+"linux-zen\n")

	if err := env.run("apply", "--packages", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	if strings.Contains(out, "linux-zen") {
		t.Fatalf("apply should not plan install for ignored package:\n%s", out)
	}
	if !strings.Contains(out, "native install: none") {
		t.Fatalf("expected no native installs:\n%s", out)
	}
}

func TestCheckDoctorReportsMalformedPackagesIgnore(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	// Path separator is rejected by validatePackageEntry / validateDirectChildName.
	writeFile(t, filepath.Join(env.repo, packagesIgnoreFile), "bad/pkg\n")

	err := env.run("check", "--doctor")
	if err == nil {
		t.Fatal("check --doctor should fail on malformed packages.ignore")
	}
	out := env.stdout.String()
	if !strings.Contains(out, "ERROR "+packagesIgnoreFile+":") {
		t.Fatalf("check --doctor should ERROR on packages.ignore:\n%s", out)
	}
}

func TestCheckDoctorIgnoresStaleIgnoredAURForHelper(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	// Only AUR intent is an ignored package — must not demand an AUR helper.
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader+"yay-bin=desc\n")
	writeFile(t, filepath.Join(env.repo, packagesIgnoreFile), generatedHeader+"yay-bin\n")

	if err := env.run("check", "--doctor"); err != nil {
		t.Fatalf("check --doctor should pass when AUR intent is fully ignored: %v\n%s", err, env.stdout.String())
	}
	if strings.Contains(env.stdout.String(), "ERROR AUR helper:") {
		t.Fatalf("ignored AUR entries must not require helper:\n%s", env.stdout.String())
	}
}
