package archstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirtyGitRepoDoesNotBlockSync(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	if err := os.MkdirAll(filepath.Join(env.repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	env.r.Now = fixedTime(2026, 6, 4, 20, 0, 0)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen)
    printf 'git\n'
    ;;
  -Qqem)
    printf ''
    ;;
  -Qi)
    shift
    printf 'Name            : git\n'
    printf 'Description     : package desc\n\n'
    ;;
  *)
    echo "unexpected pacman args: $*" >&2
    exit 2
    ;;
esac
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"old=old desc\n")

	if err := env.run("sync"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(env.repo, "pacman.conf")); !strings.Contains(got, "git=package desc\n") {
		t.Fatalf("sync did not rewrite package state:\n%s", got)
	}
	if got := readFile(t, filepath.Join(env.repo, ".snapshots", "auto-2026-06-04_20-00-00", "pacman.conf")); !strings.Contains(got, "old=old desc\n") {
		t.Fatalf("sync did not snapshot previous package state:\n%s", got)
	}
}

func TestDirtyGitRepoBlocksManagedRemoveBeforeSnapshot(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		removeName string
		conf       string
		repoTarget string
		localPath  string
	}{
		{
			name:       "config",
			command:    "config",
			removeName: "nvim",
			conf:       "config.conf",
		},
		{
			name:       "home",
			command:    "home",
			removeName: ".zshrc",
			conf:       "home.conf",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			env.initRepo(t)
			writeDirtyGit(t, env)
			env.r.Now = fixedTime(2026, 6, 4, 20, 10, 0)
			if tt.command == "config" {
				tt.repoTarget = filepath.Join(env.repo, "config", "nvim")
				tt.localPath = filepath.Join(env.home, ".config", "nvim")
			} else {
				tt.repoTarget = filepath.Join(env.repo, "home", ".zshrc")
				tt.localPath = filepath.Join(env.home, ".zshrc")
			}
			writeFile(t, filepath.Join(tt.repoTarget, "state"), "tracked\n")
			writeFile(t, filepath.Join(env.repo, tt.conf), generatedHeader+tt.removeName+"="+tt.removeName+"\n")
			if err := os.MkdirAll(filepath.Dir(tt.localPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(tt.repoTarget, tt.localPath); err != nil {
				t.Fatal(err)
			}

			err := env.run(tt.command, "rm", tt.removeName)
			assertDirtyGitError(t, err, tt.command+" rm")
			if _, statErr := os.Lstat(filepath.Join(env.repo, ".snapshots", "auto-2026-06-04_20-10-00")); !os.IsNotExist(statErr) {
				t.Fatalf("%s rm should not snapshot dirty repo state", tt.command)
			}
			if !isCorrectSymlink(tt.localPath, tt.repoTarget) {
				t.Fatalf("%s rm changed managed symlink", tt.command)
			}
		})
	}
}

func TestDirtyGitRepoBlocksSnapshotRestoreBeforeUndoSnapshot(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	env.r.Now = fixedTime(2026, 6, 4, 20, 20, 0)
	if err := env.run("snapshot", "save", "baseline"); err != nil {
		t.Fatal(err)
	}
	id := "manual-2026-06-04_20-20-00-baseline"
	writeDirtyGit(t, env)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=current\n")

	env.r.Now = fixedTime(2026, 6, 4, 20, 21, 0)
	err := env.run("snapshot", "restore", id)
	assertDirtyGitError(t, err, "snapshot restore")
	if got := readFile(t, filepath.Join(env.repo, "pacman.conf")); !strings.Contains(got, "git=current\n") {
		t.Fatalf("restore should not rewrite dirty repo state:\n%s", got)
	}
	if _, statErr := os.Lstat(filepath.Join(env.repo, ".snapshots", "auto-2026-06-04_20-21-00")); !os.IsNotExist(statErr) {
		t.Fatalf("restore should not create undo snapshot when repo is dirty")
	}
}

func TestDirtyGitRepoBlocksBootstrapRiskyManagedActions(t *testing.T) {
	tests := []struct {
		name string
		flag string
	}{
		{name: "adopt", flag: "--adopt"},
		{name: "overwrite", flag: "--overwrite"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			env.initRepo(t)
			writeDirtyGit(t, env)
			env.r.Now = fixedTime(2026, 6, 4, 20, 30, 0)
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
			writeFile(t, filepath.Join(repoTarget, "init.lua"), "tracked\n")
			local := filepath.Join(env.home, ".config", "nvim")
			writeFile(t, filepath.Join(local, "init.lua"), "local\n")

			err := env.run("bootstrap", tt.flag)
			assertDirtyGitError(t, err, "bootstrap")
			if _, statErr := os.Lstat(filepath.Join(env.repo, ".snapshots", "auto-2026-06-04_20-30-00")); !os.IsNotExist(statErr) {
				t.Fatalf("bootstrap should not snapshot dirty repo state")
			}
			info, statErr := os.Lstat(local)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				t.Fatalf("bootstrap changed local entry before dirty git check")
			}
		})
	}
}

func TestRepoLockBlocksConcurrentMutation(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFile(t, filepath.Join(env.repo, ".archstate.lock"), "sync\n")
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen)
    printf 'git\n'
    ;;
  -Qqem)
    printf ''
    ;;
  -Qi)
    echo "description query should not run when repo is locked" >&2
    exit 3
    ;;
  *)
    echo "unexpected pacman args: $*" >&2
    exit 2
    ;;
esac
`)

	err := env.run("sync")
	if err == nil {
		t.Fatal("expected lock error")
	}
	if !strings.Contains(err.Error(), "repo is locked by another archstate command") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readFile(t, filepath.Join(env.repo, ".archstate.lock")); got != "sync\n" {
		t.Fatalf("existing lock should remain untouched: %q", got)
	}
}

func TestRepoLockIsRemovedAfterCommandFailure(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	err := env.run("snapshot", "rm", "bad")
	if err == nil {
		t.Fatal("expected invalid snapshot id")
	}
	if !strings.Contains(err.Error(), "invalid snapshot id") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(env.repo, ".archstate.lock")); !os.IsNotExist(statErr) {
		t.Fatalf("lock should be removed after command failure")
	}
}

func writeDirtyGit(t *testing.T, env *testEnv) {
	t.Helper()
	writeFakeGitStatus(t, env, " M pacman.conf\n")
}

func writeFakeGitStatus(t *testing.T, env *testEnv, status string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(env.repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `
if [ "$1" = -C ] && [ "$3" = status ] && [ "$4" = --porcelain ]; then
  printf '` + status + `'
  exit 0
fi
echo "unexpected git args: $*" >&2
exit 2
`
	writeExecutable(t, filepath.Join(env.bin, "git"), body)
}

func assertDirtyGitError(t *testing.T, err error, op string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected dirty git error for %s", op)
	}
	if !strings.Contains(err.Error(), "repo has uncommitted changes") || !strings.Contains(err.Error(), "archstate "+op) {
		t.Fatalf("unexpected error for %s: %v", op, err)
	}
}
