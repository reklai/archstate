package archstate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedCommandUntracksSelectedConfigAndHome(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	// Config: nvim managed symlink
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"kitty=kitty\nnvim=nvim\n")
	writeFile(t, filepath.Join(env.repo, "config", "nvim", "init.lua"), "nvim\n")
	writeFile(t, filepath.Join(env.repo, "config", "kitty", "kitty.conf"), "kitty\n")
	if err := os.MkdirAll(filepath.Join(env.home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(env.repo, "config", "nvim"), filepath.Join(env.home, ".config", "nvim")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(env.repo, "config", "kitty"), filepath.Join(env.home, ".config", "kitty")); err != nil {
		t.Fatal(err)
	}

	// Home: .zshrc managed symlink
	writeFile(t, filepath.Join(env.repo, "home.conf"), generatedHeader+".profile=.profile\n.zshrc=.zshrc\n")
	writeFile(t, filepath.Join(env.repo, "home", ".zshrc"), "export ZSH\n")
	writeFile(t, filepath.Join(env.repo, "home", ".profile"), "export PROFILE\n")
	if err := os.Symlink(filepath.Join(env.repo, "home", ".zshrc"), filepath.Join(env.home, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(env.repo, "home", ".profile"), filepath.Join(env.home, ".profile")); err != nil {
		t.Fatal(err)
	}

	env.r.managedRemovalTUI = func(inventory managedRemovalInventory) ([]managedRemovalItem, error) {
		if len(inventory.Config) != 2 || len(inventory.Home) != 2 {
			t.Fatalf("inventory config=%d home=%d", len(inventory.Config), len(inventory.Home))
		}
		var selected []managedRemovalItem
		for _, item := range inventory.Config {
			if item.Name == "nvim" {
				selected = append(selected, item)
			}
		}
		for _, item := range inventory.Home {
			if item.Name == ".zshrc" {
				selected = append(selected, item)
			}
		}
		return selected, nil
	}

	if err := env.run("managed"); err != nil {
		t.Fatal(err)
	}

	configConf := readFile(t, filepath.Join(env.repo, "config.conf"))
	if strings.Contains(configConf, "nvim=") {
		t.Fatalf("nvim should be untracked:\n%s", configConf)
	}
	if !strings.Contains(configConf, "kitty=") {
		t.Fatalf("kitty should remain tracked:\n%s", configConf)
	}
	homeConf := readFile(t, filepath.Join(env.repo, "home.conf"))
	if strings.Contains(homeConf, ".zshrc=") {
		t.Fatalf(".zshrc should be untracked:\n%s", homeConf)
	}
	if !strings.Contains(homeConf, ".profile=") {
		t.Fatalf(".profile should remain tracked:\n%s", homeConf)
	}

	// Local copies restored (real files, not symlinks)
	if info, err := os.Lstat(filepath.Join(env.home, ".config", "nvim")); err != nil {
		t.Fatal(err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("nvim local should be restored as a real path, not a symlink")
	}
	if info, err := os.Lstat(filepath.Join(env.home, ".zshrc")); err != nil {
		t.Fatal(err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal(".zshrc local should be restored as a real file")
	}
	if pathExists(filepath.Join(env.repo, "config", "nvim")) {
		t.Fatal("tracked nvim copy should be removed from repo")
	}
	if !strings.Contains(env.stdout.String(), "stopped managing 2 entries") {
		t.Fatalf("missing success output:\n%s", env.stdout.String())
	}
}

func TestManagedCommandEmptyInventory(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	env.r.managedRemovalTUI = func(managedRemovalInventory) ([]managedRemovalItem, error) {
		t.Fatal("TUI should not open when inventory is empty")
		return nil, nil
	}
	if err := env.run("managed"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.stdout.String(), "no managed config or home entries to untrack") {
		t.Fatalf("unexpected output:\n%s", env.stdout.String())
	}
}

func TestManagedCommandNonTTYFails(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	writeFile(t, filepath.Join(env.repo, "config", "nvim", "init.lua"), "x\n")

	err := env.run("managed")
	if err == nil {
		t.Fatal("expected non-TTY error")
	}
	if !strings.Contains(err.Error(), "requires an interactive terminal") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManagedCommandTUIErrorSkipsUntrack(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	writeFile(t, filepath.Join(env.repo, "config", "nvim", "init.lua"), "x\n")
	wantErr := errors.New("tui failed")
	env.r.managedRemovalTUI = func(managedRemovalInventory) ([]managedRemovalItem, error) {
		return nil, wantErr
	}

	err := env.run("managed")
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if !strings.Contains(readFile(t, filepath.Join(env.repo, "config.conf")), "nvim=") {
		t.Fatal("state should be unchanged after TUI error")
	}
}

func TestManagedCommandNoSelection(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	writeFile(t, filepath.Join(env.repo, "config", "nvim", "init.lua"), "x\n")
	env.r.managedRemovalTUI = func(managedRemovalInventory) ([]managedRemovalItem, error) {
		return nil, nil
	}
	if err := env.run("managed"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.stdout.String(), "no entries selected") {
		t.Fatalf("unexpected output:\n%s", env.stdout.String())
	}
}

func TestManagedRemovalModelMarksAndConfirms(t *testing.T) {
	inv := managedRemovalInventory{
		Config: []managedRemovalItem{{Name: "nvim", Status: "ok", Kind: managedRemovalConfig}},
		Home:   []managedRemovalItem{{Name: ".zshrc", Status: "ok", Kind: managedRemovalHome}},
	}
	m := newManagedRemovalModel(inv)
	m.toggleItem(inv.Config[0])
	m.toggleItem(inv.Home[0])
	if len(m.marked) != 2 {
		t.Fatalf("marked = %d", len(m.marked))
	}
	selected := m.selectedItems()
	if len(selected) != 2 {
		t.Fatalf("selected = %d", len(selected))
	}
	// mark keys distinguish config vs home even if names matched
	if inv.Config[0].markKey() == inv.Home[0].markKey() {
		t.Fatal("mark keys should include kind")
	}
}
