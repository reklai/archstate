package archstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncRewritesPackagesFromPacmanAndRepairsFiles(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen)
    printf 'neovim\ngit\n'
    ;;
  -Qqem)
    printf 'paru-bin\n'
    ;;
  -Qi)
    shift
    for pkg in "$@"; do
      case "$pkg" in
        git) desc='the fast distributed version control system' ;;
        neovim) desc='vim-fork focused on extensibility and usability' ;;
        paru-bin) desc='feature packed AUR helper' ;;
        *) desc='' ;;
      esac
      printf 'Name            : %s\n' "$pkg"
      printf 'Description     : %s\n\n' "$desc"
    done
    ;;
  *)
    echo "unexpected pacman args: $*" >&2
    exit 2
    ;;
esac
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+`
# removed by sync
git=custom git note
bad line
old=old description
`)

	if err := env.run("sync"); err != nil {
		t.Fatal(err)
	}

	pacman := readFile(t, filepath.Join(env.repo, "pacman.conf"))
	if !strings.HasPrefix(pacman, generatedHeader) {
		t.Fatalf("missing generated header:\n%s", pacman)
	}
	for _, want := range []string{
		"git=custom git note\n",
		"neovim=vim-fork focused on extensibility and usability\n",
	} {
		if !strings.Contains(pacman, want) {
			t.Fatalf("pacman.conf missing %q:\n%s", want, pacman)
		}
	}
	for _, unwanted := range []string{"bad line", "old=old description", "# removed by sync"} {
		if strings.Contains(pacman, unwanted) {
			t.Fatalf("pacman.conf still contains %q:\n%s", unwanted, pacman)
		}
	}

	aur := readFile(t, filepath.Join(env.repo, "aur.conf"))
	if !strings.Contains(aur, "paru-bin=feature packed AUR helper\n") {
		t.Fatalf("aur.conf missing AUR package:\n%s", aur)
	}
}

func TestSyncSkipsSnapshotAndDescriptionQueryWhenAlreadyCurrent(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	env.r.Now = fixedTime(2026, 6, 4, 17, 0, 0)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen)
    printf 'git\n'
    ;;
  -Qqem)
    printf ''
    ;;
  -Qi)
    echo "description query should not run when state is current" >&2
    exit 3
    ;;
  *)
    echo "unexpected pacman args: $*" >&2
    exit 2
    ;;
esac
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=custom git note\n")
	writeFile(t, filepath.Join(env.repo, "aur.conf"), generatedHeader)

	if err := env.run("sync"); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(env.stdout.String(), "already synced 1 native and 0 AUR packages") {
		t.Fatalf("unexpected sync output:\n%s", env.stdout.String())
	}
	if got := readFile(t, filepath.Join(env.repo, "pacman.conf")); got != generatedHeader+"git=custom git note\n" {
		t.Fatalf("sync rewrote current package state:\n%s", got)
	}
	if _, err := os.Lstat(filepath.Join(env.repo, ".snapshots", "auto-2026-06-04_17-00-00")); !os.IsNotExist(err) {
		t.Fatalf("sync should not create an auto snapshot when state is current: %v", err)
	}
}

func TestSyncFailsWhenPacmanIsUnavailable(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	err := env.run("sync")
	if err == nil {
		t.Fatal("expected sync to fail without pacman")
	}
	if !strings.Contains(err.Error(), "pacman not found in PATH") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncDoesNotSnapshotOrRewriteWhenDescriptionQueryFails(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	env.r.Now = fixedTime(2026, 6, 4, 17, 10, 0)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen)
    printf 'git\n'
    ;;
  -Qqem)
    printf ''
    ;;
  -Qi)
    echo "pacman database is locked" >&2
    exit 1
    ;;
  *)
    echo "unexpected pacman args: $*" >&2
    exit 2
    ;;
esac
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"old=old desc\n")

	err := env.run("sync")
	if err == nil {
		t.Fatal("expected sync to fail on description query")
	}
	if !strings.Contains(err.Error(), "pacman -Qi git failed") || !strings.Contains(err.Error(), "pacman database is locked") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readFile(t, filepath.Join(env.repo, "pacman.conf")); !strings.Contains(got, "old=old desc\n") {
		t.Fatalf("sync rewrote package state after description failure:\n%s", got)
	}
	if _, statErr := os.Lstat(filepath.Join(env.repo, ".snapshots", "auto-2026-06-04_17-10-00")); !os.IsNotExist(statErr) {
		t.Fatalf("sync should not snapshot after description failure")
	}
}

func TestSyncProducesDeterministicFixedPoint(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	env.r.Now = fixedTime(2026, 6, 4, 21, 0, 0)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen)
    printf 'neovim\ngit\n'
    ;;
  -Qqem)
    printf 'paru-bin\n'
    ;;
  -Qi)
    shift
    for pkg in "$@"; do
      case "$pkg" in
        git) desc='the fast distributed version control system' ;;
        neovim) desc='vim-fork focused on extensibility and usability' ;;
        paru-bin) desc='feature packed AUR helper' ;;
        *) desc='' ;;
      esac
      printf 'Name            : %s\n' "$pkg"
      printf 'Description     : %s\n\n' "$desc"
    done
    ;;
  *)
    echo "unexpected pacman args: $*" >&2
    exit 2
    ;;
esac
`)
	// Start from a non-canonical state so the first sync must rewrite.
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=old\nbad line\n")

	if err := env.run("sync"); err != nil {
		t.Fatal(err)
	}
	firstPacman := readFile(t, filepath.Join(env.repo, "pacman.conf"))
	firstAUR := readFile(t, filepath.Join(env.repo, "aur.conf"))

	// A second sync must be a byte-for-byte no-op: sync is a deterministic fixed
	// point, independent of map iteration order.
	if err := env.run("sync"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.stdout.String(), "already synced") {
		t.Fatalf("second sync was not a no-op:\n%s", env.stdout.String())
	}
	if got := readFile(t, filepath.Join(env.repo, "pacman.conf")); got != firstPacman {
		t.Fatalf("pacman.conf is not a fixed point:\nfirst:\n%s\nsecond:\n%s", firstPacman, got)
	}
	if got := readFile(t, filepath.Join(env.repo, "aur.conf")); got != firstAUR {
		t.Fatalf("aur.conf is not a fixed point:\nfirst:\n%s\nsecond:\n%s", firstAUR, got)
	}
}

func writeFakeSyncPacman(t *testing.T, env *testEnv) {
	t.Helper()
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen) printf 'git\n' ;;
  -Qqem) printf '' ;;
  -Qi)
    shift
    printf 'Name            : git\n'
    printf 'Description     : version control\n\n'
    ;;
  *) echo "unexpected pacman args: $*" >&2; exit 2 ;;
esac
`)
}

func TestSyncCommitCommitsPackageStateInGitRepo(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	if err := os.MkdirAll(filepath.Join(env.repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	env.r.Now = fixedTime(2026, 6, 4, 22, 0, 0)
	writeFakeSyncPacman(t, env)
	gitLog := filepath.Join(env.root, "git.log")
	writeExecutable(t, filepath.Join(env.bin, "git"), `
printf '%s\n' "$*" >> `+gitLog+`
case "$3" in
  status) printf ' M pacman.conf\n' ;;
esac
exit 0
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"old=old\n")

	if err := env.run("sync", "--commit"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.stdout.String(), "synced and committed") {
		t.Fatalf("expected commit confirmation:\n%s", env.stdout.String())
	}
	log := readFile(t, gitLog)
	if !strings.Contains(log, "add -- pacman.conf aur.conf") {
		t.Fatalf("git add was not invoked for the package files:\n%s", log)
	}
	if !strings.Contains(log, "commit -m archstate sync") {
		t.Fatalf("git commit was not invoked:\n%s", log)
	}
}

func TestSyncCommitWithoutGitRepoDoesNotCommit(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	env.r.Now = fixedTime(2026, 6, 4, 22, 10, 0)
	writeFakeSyncPacman(t, env)
	writeExecutable(t, filepath.Join(env.bin, "git"), `
echo "git must not run without a .git dir: $*" >&2
exit 3
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"old=old\n")

	if err := env.run("sync", "--commit"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	if strings.Contains(out, "committed") {
		t.Fatalf("must not commit without a git repo:\n%s", out)
	}
	if !strings.Contains(out, "synced 1 native and 0 AUR packages") {
		t.Fatalf("unexpected sync output:\n%s", out)
	}
}
