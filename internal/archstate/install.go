package archstate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (r *Runner) runInstall(addToPath bool) error {
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
	return r.handlePathSetup(destDir, addToPath)
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

// handlePathSetup makes the install dir usable from a fresh shell. By default it
// only prints the exact rc file and line to add (archstate never edits shell
// files behind your back). With --add-to-path it appends that line to the rc
// file for the detected shell, idempotently.
func (r *Runner) handlePathSetup(dir string, addToPath bool) error {
	if pathContainsDir(envValue(r.Env, "PATH"), dir) {
		if addToPath {
			fmt.Fprintf(r.Stdout, "%s is already in PATH\n", homeRelativePath(r.Home, dir))
		}
		return nil
	}

	shell := detectShell(envValue(r.Env, "SHELL"))
	rcFile := r.shellRCFile(shell)
	exportLine := pathExportLine(shell, r.Home, dir)

	if addToPath {
		added, err := appendLineIfMissing(rcFile, exportLine)
		if err != nil {
			return fmt.Errorf("could not update %s: %w", homeRelativePath(r.Home, rcFile), err)
		}
		if added {
			fmt.Fprintf(r.Stdout, "added %s to PATH in %s\n", homeRelativePath(r.Home, dir), homeRelativePath(r.Home, rcFile))
			fmt.Fprintf(r.Stdout, "restart your shell or run: source %s\n", homeRelativePath(r.Home, rcFile))
		} else {
			fmt.Fprintf(r.Stdout, "%s already configured in %s\n", homeRelativePath(r.Home, dir), homeRelativePath(r.Home, rcFile))
		}
		return nil
	}

	fmt.Fprintf(r.Stdout, "%s is not in PATH\n\n", homeRelativePath(r.Home, dir))
	fmt.Fprintln(r.Stdout, "Add it automatically with:")
	fmt.Fprintln(r.Stdout, "  archstate install --add-to-path")
	fmt.Fprintf(r.Stdout, "\nor add this line to %s yourself:\n", homeRelativePath(r.Home, rcFile))
	fmt.Fprintf(r.Stdout, "  %s\n", exportLine)
	return nil
}

func detectShell(shellPath string) string {
	switch filepath.Base(strings.TrimSpace(shellPath)) {
	case "bash":
		return "bash"
	case "zsh":
		return "zsh"
	case "fish":
		return "fish"
	default:
		return "sh"
	}
}

func (r *Runner) shellRCFile(shell string) string {
	switch shell {
	case "bash":
		return filepath.Join(r.Home, ".bashrc")
	case "zsh":
		if zdotdir := envValue(r.Env, "ZDOTDIR"); zdotdir != "" {
			return filepath.Join(zdotdir, ".zshrc")
		}
		return filepath.Join(r.Home, ".zshrc")
	case "fish":
		return filepath.Join(r.Home, ".config", "fish", "config.fish")
	default:
		return filepath.Join(r.Home, ".profile")
	}
}

func pathExportLine(shell, home, dir string) string {
	target := homeEnvPath(home, dir)
	if shell == "fish" {
		return "fish_add_path " + target
	}
	return fmt.Sprintf(`export PATH="%s:$PATH"`, target)
}

// homeEnvPath renders dir as $HOME/... when it lives under home, so the rc line
// stays portable; $HOME (not ~) is used because ~ does not expand inside quotes.
func homeEnvPath(home, dir string) string {
	home = filepath.Clean(home)
	dir = filepath.Clean(dir)
	if home == "" {
		return dir
	}
	if rest, ok := strings.CutPrefix(dir, home+string(os.PathSeparator)); ok {
		return "$HOME/" + filepath.ToSlash(rest)
	}
	return dir
}

// appendLineIfMissing appends line to path (creating it and its parent dir if
// needed) unless an identical line is already present. Returns whether it wrote.
func appendLineIfMissing(path, line string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	existing := string(data)
	for have := range strings.SplitSeq(existing, "\n") {
		if strings.TrimSpace(have) == strings.TrimSpace(line) {
			return false, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return false, err
	}
	defer file.Close()
	var b strings.Builder
	if len(existing) > 0 && !strings.HasSuffix(existing, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n# Added by archstate\n")
	b.WriteString(line)
	b.WriteString("\n")
	if _, err := file.WriteString(b.String()); err != nil {
		return false, err
	}
	return true, nil
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
	if rest, ok := strings.CutPrefix(path, home+string(os.PathSeparator)); ok {
		return "~" + string(os.PathSeparator) + rest
	}
	return path
}
