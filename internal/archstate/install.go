package archstate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (r *Runner) runInstall() error {
	source, err := r.installSourcePath()
	if err != nil {
		return err
	}
	if !isExecutable(source) {
		return fmt.Errorf("install source is not executable: %s", source)
	}

	destDir := filepath.Join(r.Home, ".local", "bin")
	dest := filepath.Join(destDir, "archstate")
	installed, err := installExecutable(source, dest)
	if err != nil {
		return err
	}
	if installed {
		fmt.Fprintf(r.Stdout, "installed archstate to %s\n", homeRelativePath(r.Home, dest))
	} else {
		fmt.Fprintf(r.Stdout, "archstate is already installed at %s\n", homeRelativePath(r.Home, dest))
	}
	r.printPathHint(destDir)
	return nil
}

func (r *Runner) installSourcePath() (string, error) {
	if r.ExecutablePath != "" {
		return r.ExecutablePath, nil
	}
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return path, nil
}

func installExecutable(source, dest string) (bool, error) {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return false, err
	}
	if destInfo, err := os.Stat(dest); err == nil && os.SameFile(sourceInfo, destInfo) {
		return false, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, err
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return false, err
	}
	in, err := os.Open(source)
	if err != nil {
		return false, err
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".archstate-install-*")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return false, err
	}
	cleanup = false
	if dirFile, err := os.Open(filepath.Dir(dest)); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return true, nil
}

func (r *Runner) printPathHint(dir string) {
	if pathContainsDir(envValue(r.Env, "PATH"), dir) {
		return
	}
	fmt.Fprintf(r.Stdout, "%s is not in PATH\n\n", homeRelativePath(r.Home, dir))
	fmt.Fprintln(r.Stdout, "Add this to your shell config:")
	fmt.Fprintln(r.Stdout, `  export PATH="$HOME/.local/bin:$PATH"`)
}

func pathContainsDir(pathValue, dir string) bool {
	want, err := filepath.Abs(dir)
	if err != nil {
		want = filepath.Clean(dir)
	}
	for _, entry := range filepath.SplitList(pathValue) {
		if entry == "" {
			entry = "."
		}
		got, err := filepath.Abs(entry)
		if err != nil {
			got = filepath.Clean(entry)
		}
		if filepath.Clean(got) == filepath.Clean(want) {
			return true
		}
	}
	return false
}

func homeRelativePath(home, path string) string {
	if home == "" {
		return path
	}
	home = filepath.Clean(home)
	path = filepath.Clean(path)
	if path == home {
		return "~"
	}
	prefix := home + string(os.PathSeparator)
	if strings.HasPrefix(path, prefix) {
		return "~" + string(os.PathSeparator) + strings.TrimPrefix(path, prefix)
	}
	return path
}
