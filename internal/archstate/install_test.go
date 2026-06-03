package archstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallCopiesExecutableAndPrintsPathHint(t *testing.T) {
	env := newTestEnv(t)
	source := filepath.Join(env.root, "source-archstate")
	writeExecutable(t, source, "echo archstate\n")
	env.r.ExecutablePath = source

	if err := env.run("install"); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(env.home, ".local", "bin", "archstate")
	if !isExecutable(dest) {
		t.Fatalf("installed binary is not executable: %s", dest)
	}
	if got := readFile(t, dest); !strings.Contains(got, "echo archstate") {
		t.Fatalf("installed binary did not copy source:\n%s", got)
	}
	for _, want := range []string{
		"installed archstate to ~/.local/bin/archstate",
		"~/.local/bin is not in PATH",
		`export PATH="$HOME/.local/bin:$PATH"`,
	} {
		if !strings.Contains(env.stdout.String(), want) {
			t.Fatalf("install output missing %q:\n%s", want, env.stdout.String())
		}
	}
}

func TestInstallDoesNotPrintPathHintWhenLocalBinIsInPath(t *testing.T) {
	env := newTestEnv(t)
	source := filepath.Join(env.root, "source-archstate")
	writeExecutable(t, source, "echo archstate\n")
	env.r.ExecutablePath = source
	localBin := filepath.Join(env.home, ".local", "bin")
	env.r.Env = []string{
		"HOME=" + env.home,
		"PATH=" + localBin + string(os.PathListSeparator) + env.bin,
	}

	if err := env.run("install"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(env.stdout.String(), "is not in PATH") {
		t.Fatalf("unexpected PATH hint:\n%s", env.stdout.String())
	}
}

func TestInitInitializesRepoAndInstallsBinaryByDefault(t *testing.T) {
	env := newTestEnv(t)
	source := filepath.Join(env.root, "source-archstate")
	writeExecutable(t, source, "echo archstate\n")
	env.r.ExecutablePath = source

	if err := env.run("init"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(env.repo, ".archstate-root")); err != nil {
		t.Fatalf("repo was not initialized: %v", err)
	}
	if !isExecutable(filepath.Join(env.home, ".local", "bin", "archstate")) {
		t.Fatalf("binary was not installed")
	}
	for _, want := range []string{
		"initialized archstate repo at " + env.repo,
		"installed archstate to ~/.local/bin/archstate",
	} {
		if !strings.Contains(env.stdout.String(), want) {
			t.Fatalf("init output missing %q:\n%s", want, env.stdout.String())
		}
	}
}

func TestInitNoInstallInitializesRepoWithoutInstallingBinary(t *testing.T) {
	env := newTestEnv(t)
	source := filepath.Join(env.root, "source-archstate")
	writeExecutable(t, source, "echo archstate\n")
	env.r.ExecutablePath = source

	if err := env.run("init", "--no-install"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(env.repo, ".archstate-root")); err != nil {
		t.Fatalf("repo was not initialized: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(env.home, ".local", "bin", "archstate")); !os.IsNotExist(err) {
		t.Fatalf("binary should not have been installed")
	}
	if strings.Contains(env.stdout.String(), "installed archstate") {
		t.Fatalf("unexpected install output:\n%s", env.stdout.String())
	}
}

func TestInitRejectsUnknownFlag(t *testing.T) {
	env := newTestEnv(t)

	err := env.run("init", "--wat")
	if err == nil {
		t.Fatal("expected init with unknown flag to fail")
	}
	if !strings.Contains(err.Error(), "usage: archstate init [--no-install]") {
		t.Fatalf("unexpected error: %v", err)
	}
}
