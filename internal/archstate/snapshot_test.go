package archstate

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

	if err := env.run("track", "config", "rm", "nvim"); err != nil {
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

func TestSnapshotRestoreRemovesStateMissingFromSnapshot(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	env.r.Now = fixedTime(2026, 6, 4, 18, 0, 0)

	if err := env.run("snapshot", "save", "legacy"); err != nil {
		t.Fatal(err)
	}
	id := "manual-2026-06-04_18-00-00-legacy"
	if err := os.Remove(filepath.Join(env.repo, ".snapshots", id, "home.conf")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(env.repo, ".snapshots", id, "home")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.repo, "home.conf"), generatedHeader+".zshrc=.zshrc\n")
	writeFile(t, filepath.Join(env.repo, "home", ".zshrc"), "current home state\n")

	env.r.Now = fixedTime(2026, 6, 4, 18, 1, 0)
	if err := env.run("snapshot", "restore", id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(env.repo, "home.conf")); !os.IsNotExist(err) {
		t.Fatalf("home.conf should have been removed, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(env.repo, "home")); !os.IsNotExist(err) {
		t.Fatalf("home dir should have been removed, got %v", err)
	}
}

func TestSnapshotRestoreStagesBeforeReplacingCurrentState(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	id := "manual-2026-06-04_18-10-00-bad"
	snapshotRoot := filepath.Join(env.repo, ".snapshots", id)
	writeFile(t, filepath.Join(snapshotRoot, "pacman.conf"), generatedHeader+"git=from snapshot\n")
	writeFile(t, filepath.Join(snapshotRoot, "config.conf"), generatedHeader+"nvim=nvim\n")
	if err := os.MkdirAll(filepath.Join(snapshotRoot, "config", "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(snapshotRoot, "config", "nvim", "state.fifo"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+"git=current\n")

	env.r.Now = fixedTime(2026, 6, 4, 18, 11, 0)
	err := env.run("snapshot", "restore", id)
	if err == nil {
		t.Fatal("expected restore to fail on unsupported file type")
	}
	if !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readFile(t, filepath.Join(env.repo, "pacman.conf")); !strings.Contains(got, "git=current\n") {
		t.Fatalf("restore replaced current state before staging completed:\n%s", got)
	}
	for _, entry := range readDir(t, env.repo) {
		if strings.HasPrefix(entry.Name(), ".archstate-snapshot-restore-") {
			t.Fatalf("restore stage should have been cleaned up: %s", entry.Name())
		}
	}
}

func TestSnapshotSaveRejectsUnsupportedFileTypeAndCleansPartialSnapshot(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	if err := os.MkdirAll(filepath.Join(env.repo, "config", "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(env.repo, "config", "nvim", "state.fifo"), 0o644); err != nil {
		t.Fatal(err)
	}

	env.r.Now = fixedTime(2026, 6, 4, 18, 20, 0)
	err := env.run("snapshot", "save", "bad")
	if err == nil {
		t.Fatal("expected snapshot save to fail on unsupported file type")
	}
	if !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(env.repo, ".snapshots", "manual-2026-06-04_18-20-00-bad")); !os.IsNotExist(statErr) {
		t.Fatalf("partial snapshot should have been removed")
	}
}

func TestSnapshotIDCollisionsWithinSameSecond(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	repo, err := env.r.discoverExistingRepo()
	if err != nil {
		t.Fatal(err)
	}
	env.r.Now = fixedTime(2026, 6, 4, 18, 30, 0)

	firstAuto, err := env.r.createAutoSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	secondAuto, err := env.r.createAutoSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	if firstAuto.ID != "auto-2026-06-04_18-30-00" || secondAuto.ID != "auto-2026-06-04_18-30-00+2" {
		t.Fatalf("unexpected auto collision IDs: %q %q", firstAuto.ID, secondAuto.ID)
	}

	firstManual, err := env.r.createSnapshot(repo, "manual", "baseline")
	if err != nil {
		t.Fatal(err)
	}
	secondManual, err := env.r.createSnapshot(repo, "manual", "baseline")
	if err != nil {
		t.Fatal(err)
	}
	if firstManual.ID != "manual-2026-06-04_18-30-00-baseline" || secondManual.ID != "manual-2026-06-04_18-30-00-baseline+2" {
		t.Fatalf("unexpected manual collision IDs: %q %q", firstManual.ID, secondManual.ID)
	}
	// The collision counter must not leak into the name, and the id must
	// round-trip through parseSnapshotID (as listing reads it from disk).
	if firstManual.Name != "baseline" || secondManual.Name != "baseline" {
		t.Fatalf("unexpected manual collision names: %q %q", firstManual.Name, secondManual.Name)
	}
	reparsed, err := parseSnapshotID(secondManual.ID)
	if err != nil {
		t.Fatalf("collision id did not parse: %v", err)
	}
	if reparsed.Name != "baseline" {
		t.Fatalf("reparsed collision name = %q, want baseline", reparsed.Name)
	}
}

func TestSnapshotsPreserveExecutableBits(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	script := filepath.Join(env.repo, "home", ".local-bin")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	env.r.Now = fixedTime(2026, 6, 4, 18, 40, 0)
	if err := env.run("snapshot", "save", "perms"); err != nil {
		t.Fatal(err)
	}
	snapshotScript := filepath.Join(env.repo, ".snapshots", "manual-2026-06-04_18-40-00-perms", "home", ".local-bin")
	assertModePerm(t, snapshotScript, 0o755)

	if err := os.Chmod(script, 0o644); err != nil {
		t.Fatal(err)
	}
	env.r.Now = fixedTime(2026, 6, 4, 18, 41, 0)
	if err := env.run("snapshot", "restore", "manual-2026-06-04_18-40-00-perms"); err != nil {
		t.Fatal(err)
	}
	assertModePerm(t, script, 0o755)
}

func fixedTime(year int, month time.Month, day, hour, minute, second int) func() time.Time {
	return func() time.Time {
		return time.Date(year, month, day, hour, minute, second, 0, time.Local)
	}
}

func assertModePerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}

// TestSnapshotStateNamesCoversRepoState pins the invariant that snapshots
// capture exactly the repo-state components a fresh repo creates (minus the
// marker). Adding a new state file without updating snapshotStateNames (or
// vice versa) fails here, preventing silent snapshot/restore data loss.
func TestSnapshotStateNamesCoversRepoState(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	captured := make(map[string]bool)
	for _, name := range snapshotStateNames() {
		captured[name] = true
	}

	for _, entry := range readDir(t, env.repo) {
		name := entry.Name()
		switch name {
		case markerFile, ".snapshots", ".git":
			continue
		}
		if !captured[name] {
			t.Errorf("repo state %q is created by init but not captured by snapshotStateNames()", name)
		}
		delete(captured, name)
	}
	for name := range captured {
		t.Errorf("snapshotStateNames() lists %q which a fresh repo does not create", name)
	}
}
