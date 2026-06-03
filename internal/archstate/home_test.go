package archstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesHomeState(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	for _, path := range []string{
		filepath.Join(env.repo, "home.conf"),
		filepath.Join(env.repo, "home"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
}

func TestHomeAddAdoptsExistingHomeFile(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFile(t, filepath.Join(env.home, ".zshrc"), "export EDITOR=nvim\n")

	if err := env.run("home", "add", ".zshrc"); err != nil {
		t.Fatal(err)
	}
	repoTarget := filepath.Join(env.repo, "home", ".zshrc")
	if got := readFile(t, repoTarget); got != "export EDITOR=nvim\n" {
		t.Fatalf("repo target did not get home file: %q", got)
	}
	local := filepath.Join(env.home, ".zshrc")
	link, err := os.Readlink(local)
	if err != nil {
		t.Fatal(err)
	}
	if link != repoTarget {
		t.Fatalf("wrong symlink target: %s", link)
	}
	conf := readFile(t, filepath.Join(env.repo, "home.conf"))
	if !strings.Contains(conf, ".zshrc=.zshrc\n") {
		t.Fatalf("home.conf missing mapping:\n%s", conf)
	}
}

func TestHomeAddRejectsNestedPath(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	err := env.run("home", "add", ".ssh/config")
	if err == nil {
		t.Fatal("expected nested home path to be rejected")
	}
	if !strings.Contains(err.Error(), "must be a direct child name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHomeRemoveRestoresLocalFileAndRemovesRepoData(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	repoTarget := filepath.Join(env.repo, "home", ".zshrc")
	writeFile(t, repoTarget, "export EDITOR=nvim\n")
	writeFile(t, filepath.Join(env.repo, "home.conf"), generatedHeader+".zshrc=.zshrc\n")
	local := filepath.Join(env.home, ".zshrc")
	if err := os.Symlink(repoTarget, local); err != nil {
		t.Fatal(err)
	}

	if err := env.run("home", "rm", ".zshrc"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(local)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected local file to be restored as a real file")
	}
	if got := readFile(t, local); got != "export EDITOR=nvim\n" {
		t.Fatalf("local file was not restored: %q", got)
	}
	if _, err := os.Lstat(repoTarget); !os.IsNotExist(err) {
		t.Fatalf("expected repo data to be removed")
	}
	conf := readFile(t, filepath.Join(env.repo, "home.conf"))
	if strings.Contains(conf, ".zshrc=.zshrc") {
		t.Fatalf("mapping was not removed:\n%s", conf)
	}
}

func TestBootstrapPreviewIncludesHomeFiles(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "home.conf"), generatedHeader+".zshrc=.zshrc\n")
	repoTarget := filepath.Join(env.repo, "home", ".zshrc")
	writeFile(t, repoTarget, "export EDITOR=nvim\n")

	if err := env.run("bootstrap", "--preview"); err != nil {
		t.Fatal(err)
	}
	want := "link " + filepath.Join(env.home, ".zshrc") + " -> " + repoTarget
	if !strings.Contains(env.stdout.String(), "Home file plan:") || !strings.Contains(env.stdout.String(), want) {
		t.Fatalf("preview did not include home file link %q:\n%s", want, env.stdout.String())
	}
}

func TestStatusTreatsMissingHomeConfAsEmptyForOlderRepos(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	if err := os.Remove(filepath.Join(env.repo, "home.conf")); err != nil {
		t.Fatal(err)
	}
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen)
    printf ''
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

	if err := env.run("status"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.stdout.String(), "Home file status:\n  no home files declared") {
		t.Fatalf("status did not treat missing home.conf as empty:\n%s", env.stdout.String())
	}
}

func TestBootstrapHomeAdoptReplacesTrackedCopy(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "home.conf"), generatedHeader+".zshrc=.zshrc\n")
	repoTarget := filepath.Join(env.repo, "home", ".zshrc")
	writeFile(t, repoTarget, "old tracked copy\n")
	local := filepath.Join(env.home, ".zshrc")
	writeFile(t, local, "current home file\n")

	if err := env.run("bootstrap", "--adopt"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, repoTarget); got != "current home file\n" {
		t.Fatalf("tracked copy was not replaced: %q", got)
	}
	target, err := os.Readlink(local)
	if err != nil {
		t.Fatal(err)
	}
	if target != repoTarget {
		t.Fatalf("wrong symlink target: %s", target)
	}
}

func TestBootstrapHomeOverwriteRestoresTrackedCopy(t *testing.T) {
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
	writeFile(t, filepath.Join(env.repo, "home.conf"), generatedHeader+".zshrc=.zshrc\n")
	repoTarget := filepath.Join(env.repo, "home", ".zshrc")
	writeFile(t, repoTarget, "tracked copy\n")
	local := filepath.Join(env.home, ".zshrc")
	writeFile(t, local, "unmanaged local copy\n")

	if err := env.run("bootstrap", "--overwrite"); err != nil {
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
