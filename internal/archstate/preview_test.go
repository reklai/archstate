package archstate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func row(status, name string) string {
	return fmt.Sprintf("  %-8s %s", status, name)
}

func TestConfigPreviewClassifiesEntriesAndExcludesRepo(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	// tracked: nvim is in config.conf and present under ~/.config
	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	repoNvim := filepath.Join(env.repo, "config", "nvim")
	writeFile(t, filepath.Join(repoNvim, "init.lua"), "tracked\n")
	if err := os.Symlink(repoNvim, filepath.Join(env.home, ".config", "nvim")); err != nil {
		t.Fatal(err)
	}
	// addable: a real dir not tracked
	writeFile(t, filepath.Join(env.home, ".config", "kitty", "kitty.conf"), "x\n")
	// untracked symlink: not adoptable as-is
	if err := os.Symlink(filepath.Join(env.root, "elsewhere"), filepath.Join(env.home, ".config", "dangling")); err != nil {
		t.Fatal(err)
	}

	if err := env.run("track", "config", "preview"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()

	for _, want := range []string{
		"config entries under ~/.config:",
		row("tracked", "nvim"),
		row("add", "kitty"),
		row("symlink", "dangling"),
		"add with: archstate config add <name>",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("config preview missing %q:\n%s", want, out)
		}
	}
	// the archstate repo dir itself must never be offered as a target
	if strings.Contains(out, row("add", "archstate")) {
		t.Fatalf("config preview should exclude the archstate repo dir:\n%s", out)
	}
}

func TestHomePreviewShowsDotfilesAndExcludesNoise(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	writeFile(t, filepath.Join(env.repo, "home.conf"), generatedHeader+".zshrc=.zshrc\n")
	writeFile(t, filepath.Join(env.home, ".zshrc"), "tracked\n")      // tracked
	writeFile(t, filepath.Join(env.home, ".profile"), "addable\n")    // addable
	writeFile(t, filepath.Join(env.home, ".cache", "x"), "noise\n")   // excluded dotfile
	writeFile(t, filepath.Join(env.home, "Documents", "x"), "real\n") // non-dotfile

	if err := env.run("track", "home", "preview"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()

	for _, want := range []string{
		"home entries under ~:",
		row("tracked", ".zshrc"),
		row("add", ".profile"),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("home preview missing %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{
		row("add", ".config"), // managed via `config`, excluded
		row("add", ".cache"),  // noise, excluded
		"Documents",           // non-dotfile, filtered out
	} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("home preview should not show %q:\n%s", unwanted, out)
		}
	}
}
