package archstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeAddDeniesSensitiveByDefault(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFile(t, filepath.Join(env.home, ".ssh", "id_ed25519"), "secret\n")

	err := env.run("home", "add", ".ssh")
	if err == nil {
		t.Fatal("expected sensitive deny")
	}
	if !strings.Contains(err.Error(), "sensitive") || !strings.Contains(err.Error(), "--force-sensitive") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(env.home, ".ssh")); err != nil {
		t.Fatalf(".ssh should remain local: %v", err)
	}
}

func TestHomeAddForceSensitiveAllowsDenyList(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFile(t, filepath.Join(env.home, ".ssh", "config"), "Host *\n")

	if err := env.run("home", "add", "--force-sensitive", ".ssh"); err != nil {
		t.Fatalf("force-sensitive add: %v", err)
	}
	if !isCorrectSymlink(filepath.Join(env.home, ".ssh"), filepath.Join(env.repo, "home", ".ssh")) {
		t.Fatal("expected managed symlink for .ssh")
	}
}

func TestHomePreviewLabelsSensitiveAsDeny(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFile(t, filepath.Join(env.home, ".ssh", "config"), "Host *\n")
	writeFile(t, filepath.Join(env.home, ".profile"), "export PATH\n")

	if err := env.run("home", "preview"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	if !strings.Contains(out, row("deny", ".ssh")) {
		t.Fatalf("preview missing deny for .ssh:\n%s", out)
	}
	if !strings.Contains(out, row("add", ".profile")) {
		t.Fatalf("preview missing add for .profile:\n%s", out)
	}
}

func TestCustomSensitiveDenyFile(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFile(t, filepath.Join(env.repo, sensitiveDenyFile), "custom-secret\n")
	writeFile(t, filepath.Join(env.home, ".config", "custom-secret"), "nope\n")

	err := env.run("config", "add", "custom-secret")
	if err == nil {
		t.Fatal("expected custom deny")
	}
	if !strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBootstrapAdoptDeniesSensitive(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qq) printf '' ;;
  *) echo "unexpected pacman args: $*" >&2; exit 2 ;;
esac
`)
	writeFile(t, filepath.Join(env.repo, "home.conf"), generatedHeader+".ssh=.ssh\n")
	// tracked copy missing, local real dir → adopt path
	writeFile(t, filepath.Join(env.home, ".ssh", "config"), "Host *\n")

	err := env.run("bootstrap", "--dotfiles", "--adopt")
	if err == nil {
		t.Fatal("expected bootstrap adopt to fail on sensitive")
	}
	if !strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("unexpected error: %v", err)
	}
}
