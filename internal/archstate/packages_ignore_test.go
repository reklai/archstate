package archstate

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPackagesIgnoreAddListRemove(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	if err := env.run("packages", "ignore", "add", "linux-zen", "nvidia"); err != nil {
		t.Fatal(err)
	}
	if err := env.run("packages", "ignore", "list"); err != nil {
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

	if err := env.run("packages", "ignore", "rm", "nvidia"); err != nil {
		t.Fatal(err)
	}
	if err := env.run("packages", "ignore", "list"); err != nil {
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

func TestStatusAndVerifyIgnoreUntrackedIgnoredPackages(t *testing.T) {
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

	if err := env.run("status"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(env.stdout.String(), "linux-zen") {
		t.Fatalf("status should not report ignored package as untracked:\n%s", env.stdout.String())
	}
	if err := env.run("verify", "--strict-packages"); err != nil {
		t.Fatalf("strict verify should pass when only ignored packages are extra: %v\n%s", err, env.stdout.String())
	}
}

func TestBootstrapDoesNotInstallIgnoredTrackedPackages(t *testing.T) {
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

	if err := env.run("bootstrap", "--packages", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	if strings.Contains(out, "linux-zen") {
		t.Fatalf("bootstrap should not plan install for ignored package:\n%s", out)
	}
	if !strings.Contains(out, "native install: none") {
		t.Fatalf("expected no native installs:\n%s", out)
	}
}

func TestDoctorReportsMalformedPackagesIgnore(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	// Path separator is rejected by validatePackageEntry / validateDirectChildName.
	writeFile(t, filepath.Join(env.repo, packagesIgnoreFile), "bad/pkg\n")

	err := env.run("doctor")
	if err == nil {
		t.Fatal("doctor should fail on malformed packages.ignore")
	}
	out := env.stdout.String()
	if !strings.Contains(out, "ERROR "+packagesIgnoreFile+":") {
		t.Fatalf("doctor should ERROR on packages.ignore:\n%s", out)
	}
}

func TestDoctorIgnoresStaleIgnoredAURForHelper(t *testing.T) {
	env := newTestEnv(t)
	setupCleanMachine(t, env)
	// Only AUR intent is an ignored package — must not demand an AUR helper.
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader+"yay-bin=desc\n")
	writeFile(t, filepath.Join(env.repo, packagesIgnoreFile), generatedHeader+"yay-bin\n")

	if err := env.run("doctor"); err != nil {
		t.Fatalf("doctor should pass when AUR intent is fully ignored: %v\n%s", err, env.stdout.String())
	}
	if strings.Contains(env.stdout.String(), "ERROR AUR helper:") {
		t.Fatalf("ignored AUR entries must not require helper:\n%s", env.stdout.String())
	}
}
