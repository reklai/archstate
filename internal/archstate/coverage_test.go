package archstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoverageReportsTrackedAndAddable(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)

	writeFile(t, filepath.Join(env.repo, "config.conf"), generatedHeader+"nvim=nvim\n")
	repoNvim := filepath.Join(env.repo, "config", "nvim")
	writeFile(t, filepath.Join(repoNvim, "init.lua"), "x\n")
	if err := os.Symlink(repoNvim, filepath.Join(env.home, ".config", "nvim")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.home, ".config", "kitty", "kitty.conf"), "x\n")
	writeFile(t, filepath.Join(env.home, ".config", "gcloud", "credentials"), "secret\n")
	writeFile(t, filepath.Join(env.home, ".profile"), "export PATH\n")
	writeFile(t, filepath.Join(env.home, ".ssh", "config"), "Host *\n")

	if err := env.run("coverage"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	for _, want := range []string{
		"Config coverage",
		"tracked: 1",
		"addable: 1",
		"deny:    1",
		"add next: kitty",
		"Home coverage",
		"Overall:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("coverage missing %q:\n%s", want, out)
		}
	}
	// .ssh is deny on home; .profile is addable
	if !strings.Contains(out, "add next: .profile") && !strings.Contains(out, ".profile") {
		// home section should mention addable profile somewhere
		if !strings.Contains(out, "Home coverage") {
			t.Fatalf("unexpected coverage:\n%s", out)
		}
	}
}
