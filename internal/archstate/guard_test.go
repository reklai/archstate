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

func TestDirtyGitRepoDoesNotBlockManagedAdd(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeDirtyGit(t, env)
	local := filepath.Join(env.home, ".config", "starship.toml")
	writeFile(t, local, "local\n")

	if err := env.run("config", "add", "starship.toml"); err != nil {
		t.Fatal(err)
	}
	repoTarget := filepath.Join(env.repo, "config", "starship.toml")
	if !isCorrectSymlink(local, repoTarget) {
		t.Fatalf("config add should replace local entry with managed symlink")
	}
	if got := readFile(t, repoTarget); got != "local\n" {
		t.Fatalf("adopted config = %q, want local contents", got)
	}
	if got := readFile(t, filepath.Join(env.repo, "config.conf")); !strings.Contains(got, "starship.toml=starship.toml\n") {
		t.Fatalf("config.conf missing adopted entry:\n%s", got)
	}
}

func TestDirtyGitRepoDoesNotBlockManagedRemove(t *testing.T) {
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

			if err := env.run(tt.command, "rm", tt.removeName); err != nil {
				t.Fatal(err)
			}
			snapshotRoot := filepath.Join(env.repo, ".snapshots", "auto-2026-06-04_20-10-00")
			if got := readFile(t, filepath.Join(snapshotRoot, tt.conf)); !strings.Contains(got, tt.removeName+"="+tt.removeName+"\n") {
				t.Fatalf("%s rm snapshot did not preserve pre-remove state:\n%s", tt.command, got)
			}
			if pathExists(tt.repoTarget) {
				t.Fatalf("%s rm should remove repo target", tt.command)
			}
			info, statErr := os.Lstat(tt.localPath)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				t.Fatalf("%s rm should restore local entry as a real file or directory", tt.command)
			}
		})
	}
}

func TestDirtyGitRepoDoesNotBlockSnapshotRestore(t *testing.T) {
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
	if err := env.run("snapshot", "restore", id); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(env.repo, "pacman.conf")); strings.Contains(got, "git=current\n") {
		t.Fatalf("restore did not replace dirty current state:\n%s", got)
	}
	if got := readFile(t, filepath.Join(env.repo, ".snapshots", "auto-2026-06-04_20-21-00", "pacman.conf")); !strings.Contains(got, "git=current\n") {
		t.Fatalf("restore undo snapshot did not preserve dirty current state:\n%s", got)
	}
}

func TestDirtyGitRepoDoesNotBlockBootstrapRiskyManagedActions(t *testing.T) {
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

			if err := env.run("bootstrap", tt.flag); err != nil {
				t.Fatal(err)
			}
			snapshotTarget := filepath.Join(env.repo, ".snapshots", "auto-2026-06-04_20-30-00", "config", "nvim", "init.lua")
			if got := readFile(t, snapshotTarget); got != "tracked\n" {
				t.Fatalf("bootstrap snapshot = %q, want pre-change tracked state", got)
			}
			if !isCorrectSymlink(local, repoTarget) {
				t.Fatalf("bootstrap should leave local entry as managed symlink")
			}
			if tt.flag == "--adopt" {
				if got := readFile(t, filepath.Join(repoTarget, "init.lua")); got != "local\n" {
					t.Fatalf("adopted repo target = %q, want local state", got)
				}
			} else {
				if got := readFile(t, filepath.Join(repoTarget, "init.lua")); got != "tracked\n" {
					t.Fatalf("overwritten repo target = %q, want tracked state", got)
				}
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
