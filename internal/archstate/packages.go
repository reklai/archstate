package archstate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func buildPackageState(names []string, existing map[string]string, descriptions map[string]string) map[string]string {
	state := make(map[string]string, len(names))
	for _, name := range names {
		if value, ok := existing[name]; ok {
			state[name] = value
			continue
		}
		state[name] = descriptions[name]
	}
	return state
}

func (r *Runner) queryPackageNames(name string, args ...string) ([]string, error) {
	out, err := r.commandOutput(name, args...)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		names = append(names, line)
	}
	sort.Strings(names)
	return names, nil
}

func (r *Runner) queryPackageDescriptions(names []string) (map[string]string, error) {
	descriptions := make(map[string]string)
	names = uniqueSorted(names)
	for start := 0; start < len(names); start += 100 {
		end := start + 100
		if end > len(names) {
			end = len(names)
		}
		args := append([]string{"-Qi"}, names[start:end]...)
		out, err := r.commandOutput("pacman", args...)
		if err != nil {
			return nil, err
		}
		for name, description := range parsePacmanInfo(out) {
			descriptions[name] = description
		}
	}
	for _, name := range names {
		if _, ok := descriptions[name]; !ok {
			descriptions[name] = ""
		}
	}
	return descriptions, nil
}

func parsePacmanInfo(out string) map[string]string {
	descriptions := make(map[string]string)
	var name, description string
	commit := func() {
		if name != "" {
			descriptions[name] = description
		}
		name = ""
		description = ""
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			commit()
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Name":
			if name != "" {
				commit()
			}
			name = strings.TrimSpace(value)
		case "Description":
			description = strings.TrimSpace(value)
		}
	}
	commit()
	return descriptions
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func packageNames(entries map[string]string) []string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func missingPackages(wanted map[string]string, installed []string) []string {
	installedSet := make(map[string]bool, len(installed))
	for _, name := range installed {
		installedSet[name] = true
	}
	missing := make([]string, 0)
	for name := range wanted {
		if !installedSet[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

func (r *Runner) findAURHelper() (string, error) {
	if err := r.requireCommand("paru"); err == nil {
		return "paru", nil
	}
	if err := r.requireCommand("yay"); err == nil {
		return "yay", nil
	}
	return "", errors.New("AUR packages are declared but neither paru nor yay is installed")
}

func (r *Runner) requireCommand(name string) error {
	_, err := r.lookPath(name)
	return err
}

func (r *Runner) commandOutput(name string, args ...string) (string, error) {
	path, err := r.lookPath(name)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(path, args...)
	cmd.Env = r.Env
	cmd.Dir = r.Cwd
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, msg)
		}
		return "", fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return string(out), nil
}

func (r *Runner) streamCommand(name string, args ...string) error {
	path, err := r.lookPath(name)
	if err != nil {
		return err
	}
	cmd := exec.Command(path, args...)
	cmd.Env = r.Env
	cmd.Dir = r.Cwd
	cmd.Stdin = r.Stdin
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func (r *Runner) lookPath(name string) (string, error) {
	if filepath.IsAbs(name) {
		if isExecutable(name) {
			return name, nil
		}
		return "", fmt.Errorf("%s is not executable", name)
	}
	pathValue := envValue(r.Env, "PATH")
	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, name)
		if isExecutable(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s not found in PATH", name)
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimPrefix(env[i], prefix)
		}
	}
	return os.Getenv(key)
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}
