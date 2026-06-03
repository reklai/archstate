package archstate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testEnv struct {
	r      *Runner
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	root   string
	home   string
	cwd    string
	bin    string
	repo   string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cwd := filepath.Join(root, "work")
	bin := filepath.Join(root, "bin")
	fallbackBin := filepath.Join(root, "usr-bin")
	for _, dir := range []string{home, cwd, bin, fallbackBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	env := []string{
		"HOME=" + home,
		"PATH=" + bin,
		"ARCHSTATE_AUR_HELPER_FALLBACK_DIR=" + fallbackBin,
	}
	return &testEnv{
		r: &Runner{
			Stdin:  strings.NewReader(""),
			Stdout: stdout,
			Stderr: stderr,
			Cwd:    cwd,
			Home:   home,
			Env:    env,
		},
		stdout: stdout,
		stderr: stderr,
		root:   root,
		home:   home,
		cwd:    cwd,
		bin:    bin,
		repo:   filepath.Join(home, ".config", "archstate"),
	}
}

func (e *testEnv) run(args ...string) error {
	e.stdout.Reset()
	e.stderr.Reset()
	return e.r.Run(args)
}

func (e *testEnv) initRepo(t *testing.T) {
	t.Helper()
	if err := e.run("init", "--no-install"); err != nil {
		t.Fatal(err)
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	data := "#!/bin/sh\nset -eu\n" + body
	if err := os.WriteFile(path, []byte(data), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFakePacman(t *testing.T, bin, body string) {
	t.Helper()
	writeExecutable(t, filepath.Join(bin, "pacman"), body)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
