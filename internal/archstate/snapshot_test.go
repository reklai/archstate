package archstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSnapshotSaveListRestoreAndRemove(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	env.r.Now = fixedTime(2026, 6, 4, 14, 30, 11)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=old desc\n")
	writeFile(t, filepath.Join(env.repo, "config", "nvim", "init.lua"), "old config\n")

	if err := env.run("snapshot", "save", "baseline"); err != nil {
		t.Fatal(err)
	}
	id := "manual-2026-06-04_14-30-11-baseline"
	if !strings.Contains(env.stdout.String(), "saved snapshot "+id+"  2026/06/04-14:30:11  baseline") {
		t.Fatalf("unexpected save output:\n%s", env.stdout.String())
	}

	if err := env.run("snapshot", "list"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ID                                                      NAME              TYPE    TIME",
		"manual  2026/06/04-14:30:11",
		id,
		"baseline",
	} {
		if !strings.Contains(env.stdout.String(), want) {
			t.Fatalf("snapshot list missing %q:\n%s", want, env.stdout.String())
		}
	}
	if strings.Index(env.stdout.String(), id) > strings.Index(env.stdout.String(), "baseline") {
		t.Fatalf("snapshot list should show ID before name:\n%s", env.stdout.String())
	}

	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=new desc\n")
	if err := os.RemoveAll(filepath.Join(env.repo, "config", "nvim")); err != nil {
		t.Fatal(err)
	}
	env.r.Now = fixedTime(2026, 6, 4, 14, 31, 0)
	if err := env.run("snapshot", "restore", id); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(env.repo, "pacman.conf")); !strings.Contains(got, "git=old desc\n") {
		t.Fatalf("pacman.conf was not restored:\n%s", got)
	}
	if got := readFile(t, filepath.Join(env.repo, "config", "nvim", "init.lua")); got != "old config\n" {
		t.Fatalf("config was not restored: %q", got)
	}
	if got := readFile(t, filepath.Join(env.repo, ".snapshots", "auto-2026-06-04_14-31-00", "pacman.conf")); !strings.Contains(got, "git=new desc\n") {
		t.Fatalf("restore did not create undo auto snapshot:\n%s", got)
	}

	if err := env.run("snapshot", "rm", id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(env.repo, ".snapshots", id)); !os.IsNotExist(err) {
		t.Fatalf("manual snapshot was not removed")
	}
}

func TestSyncCreatesAutoSnapshotBeforePackageRewrite(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	env.r.Now = fixedTime(2026, 6, 4, 15, 0, 0)
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
    printf 'Description     : new package desc\n\n'
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

	snapshotPacman := readFile(t, filepath.Join(env.repo, ".snapshots", "auto-2026-06-04_15-00-00", "pacman.conf"))
	if !strings.Contains(snapshotPacman, "old=old desc\n") {
		t.Fatalf("auto snapshot did not capture pre-sync state:\n%s", snapshotPacman)
	}
	currentPacman := readFile(t, filepath.Join(env.repo, "pacman.conf"))
	if !strings.Contains(currentPacman, "git=new package desc\n") || strings.Contains(currentPacman, "old=old desc") {
		t.Fatalf("sync did not rewrite current package state:\n%s", currentPacman)
	}
}

func TestAutoSnapshotsKeepLatestFiveAndManualSnapshots(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	repo, err := env.r.discoverExistingRepo()
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 6, 4, 16, 0, 0, 0, time.Local)

	env.r.Now = func() time.Time { return base }
	if _, err := env.r.createSnapshot(repo, "manual", "baseline"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		current := base.Add(time.Duration(i) * time.Second)
		env.r.Now = func() time.Time { return current }
		if _, err := env.r.createAutoSnapshot(repo); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := os.Lstat(filepath.Join(env.repo, ".snapshots", "manual-2026-06-04_16-00-00-baseline")); err != nil {
		t.Fatalf("manual snapshot should not be pruned: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(env.repo, ".snapshots", "auto-2026-06-04_16-00-00")); !os.IsNotExist(err) {
		t.Fatalf("oldest auto snapshot should have been pruned")
	}
	for second := 1; second <= 5; second++ {
		id := base.Add(time.Duration(second) * time.Second).Format("auto-" + snapshotTimeLayout)
		if _, err := os.Lstat(filepath.Join(env.repo, ".snapshots", id)); err != nil {
			t.Fatalf("expected auto snapshot %s to remain: %v", id, err)
		}
	}
}

func TestSnapshotListFiltersManualAndAuto(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	repo, err := env.r.discoverExistingRepo()
	if err != nil {
		t.Fatal(err)
	}
	env.r.Now = fixedTime(2026, 6, 4, 16, 30, 0)
	if _, err := env.r.createSnapshot(repo, "manual", "baseline"); err != nil {
		t.Fatal(err)
	}
	env.r.Now = fixedTime(2026, 6, 4, 16, 31, 0)
	if _, err := env.r.createAutoSnapshot(repo); err != nil {
		t.Fatal(err)
	}

	if err := env.run("snapshot", "list", "--manual"); err != nil {
		t.Fatal(err)
	}
	manualOut := env.stdout.String()
	if !strings.Contains(manualOut, "manual-2026-06-04_16-30-00-baseline") || strings.Contains(manualOut, "auto-2026-06-04_16-31-00") {
		t.Fatalf("manual filter output was wrong:\n%s", manualOut)
	}

	if err := env.run("snapshot", "list", "--auto"); err != nil {
		t.Fatal(err)
	}
	autoOut := env.stdout.String()
	if !strings.Contains(autoOut, "auto-2026-06-04_16-31-00") || strings.Contains(autoOut, "manual-2026-06-04_16-30-00-baseline") {
		t.Fatalf("auto filter output was wrong:\n%s", autoOut)
	}

	err = env.run("snapshot", "list", "--manual", "--auto")
	if err == nil {
		t.Fatal("expected mutually exclusive filters to fail")
	}
	if !strings.Contains(err.Error(), "--manual and --auto are mutually exclusive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigRemoveCreatesAutoSnapshotBeforeRemoval(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	env.r.Now = fixedTime(2026, 6, 4, 17, 0, 0)
	repoTarget := filepath.Join(env.repo, "config", "nvim")
	writeFile(t, filepath.Join(repoTarget, "init.lua"), "tracked config\n")
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	local := filepath.Join(env.home, ".config", "nvim")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repoTarget, local); err != nil {
		t.Fatal(err)
	}

	if err := env.run("config", "rm", "nvim"); err != nil {
		t.Fatal(err)
	}

	snapshotRoot := filepath.Join(env.repo, ".snapshots", "auto-2026-06-04_17-00-00")
	if got := readFile(t, filepath.Join(snapshotRoot, "config", "nvim", "init.lua")); got != "tracked config\n" {
		t.Fatalf("auto snapshot did not capture config before removal: %q", got)
	}
	if conf := readFile(t, filepath.Join(snapshotRoot, "config.conf")); !strings.Contains(conf, "nvim=nvim\n") {
		t.Fatalf("auto snapshot did not capture config.conf mapping:\n%s", conf)
	}
}

func TestSnapshotSaveRequiresName(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	err := env.run("snapshot", "save")
	if err == nil {
		t.Fatal("expected snapshot save without a name to fail")
	}
	if !strings.Contains(err.Error(), "usage: archstate snapshot save <name>") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func fixedTime(year int, month time.Month, day, hour, minute, second int) func() time.Time {
	return func() time.Time {
		return time.Date(year, month, day, hour, minute, second, 0, time.Local)
	}
}
