package archstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigAddMultipleNamesInOneCommand(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFile(t, filepath.Join(env.home, ".config", "nvim", "init.lua"), "n\n")
	writeFile(t, filepath.Join(env.home, ".config", "kitty", "kitty.conf"), "k\n")

	if err := env.run("config", "add", "nvim", "kitty"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nvim", "kitty"} {
		if !isCorrectSymlink(filepath.Join(env.home, ".config", name), filepath.Join(env.repo, "config", name)) {
			t.Fatalf("%s was not adopted as a managed symlink", name)
		}
	}
	conf := readFile(t, filepath.Join(env.repo, "config.conf"))
	for _, want := range []string{"kitty=kitty\n", "nvim=nvim\n"} {
		if !strings.Contains(conf, want) {
			t.Fatalf("config.conf missing %q:\n%s", want, conf)
		}
	}
}

func TestConfigAddIsAllOrNothingWhenOneConflicts(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFile(t, filepath.Join(env.home, ".config", "nvim", "init.lua"), "n\n")      // adoptable
	writeFile(t, filepath.Join(env.home, ".config", "kitty", "kitty.conf"), "k\n")   // local exists
	writeFile(t, filepath.Join(env.repo, "config", "kitty", "old.conf"), "exists\n") // repo target already exists -> conflict

	err := env.run("config", "add", "nvim", "kitty")
	if err == nil || !strings.Contains(err.Error(), "repo target already exists") {
		t.Fatalf("expected a conflict abort, got: %v", err)
	}
	// the good entry must not have been touched (validate-all-then-apply)
	if pathExists(filepath.Join(env.repo, "config", "nvim")) {
		t.Fatalf("nvim should not have been adopted when the batch aborts")
	}
	info, statErr := os.Lstat(filepath.Join(env.home, ".config", "nvim"))
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("nvim local entry should remain a real dir, not be moved")
	}
	if conf := readFile(t, filepath.Join(env.repo, "config.conf")); strings.Contains(conf, "nvim") || strings.Contains(conf, "kitty") {
		t.Fatalf("config.conf should be unchanged:\n%s", conf)
	}
}

func TestConfigAddRejectsFlagLikeAndDuplicateNames(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	if err := env.run("config", "add", "--overwrite", "nvim"); err == nil || !strings.Contains(err.Error(), "flag-like argument") {
		t.Fatalf("expected flag-like guard, got: %v", err)
	}
	if err := env.run("config", "add", "nvim", "nvim"); err == nil || !strings.Contains(err.Error(), "duplicate name") {
		t.Fatalf("expected duplicate guard, got: %v", err)
	}
}

func TestConfigRemoveMultipleNamesTakesOneSnapshot(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	env.r.Now = fixedTime(2026, 6, 4, 23, 0, 0)
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"kitty=kitty\nnvim=nvim\n")
	for _, name := range []string{"nvim", "kitty"} {
		repoTarget := filepath.Join(env.repo, "config", name)
		writeFile(t, filepath.Join(repoTarget, name+".conf"), "tracked\n")
		local := filepath.Join(env.home, ".config", name)
		if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(repoTarget, local); err != nil {
			t.Fatal(err)
		}
	}

	if err := env.run("config", "rm", "nvim", "kitty"); err != nil {
		t.Fatal(err)
	}
	if conf := readFile(t, filepath.Join(env.repo, "config.conf")); strings.Contains(conf, "nvim") || strings.Contains(conf, "kitty") {
		t.Fatalf("config.conf should have neither entry:\n%s", conf)
	}
	for _, name := range []string{"nvim", "kitty"} {
		if pathExists(filepath.Join(env.repo, "config", name)) {
			t.Fatalf("repo target for %s should be removed", name)
		}
		info, err := os.Lstat(filepath.Join(env.home, ".config", name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("%s should be restored as a real entry, not a symlink", name)
		}
	}
	if snaps := readDir(t, filepath.Join(env.repo, ".snapshots")); len(snaps) != 1 {
		t.Fatalf("expected one batch snapshot, got %d", len(snaps))
	}
}

func TestHomeAddMultipleNamesInOneCommand(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFile(t, filepath.Join(env.home, ".zshrc"), "z\n")
	writeFile(t, filepath.Join(env.home, ".profile"), "p\n")

	if err := env.run("home", "add", ".zshrc", ".profile"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".zshrc", ".profile"} {
		if !isCorrectSymlink(filepath.Join(env.home, name), filepath.Join(env.repo, "home", name)) {
			t.Fatalf("%s was not adopted as a managed symlink", name)
		}
	}
}
