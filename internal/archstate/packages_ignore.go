package archstate

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
)

func formatIgnoreList(names []string) []byte {
	names = uniqueSorted(names)
	var b strings.Builder
	b.WriteString(generatedHeader)
	for _, name := range names {
		b.WriteString(name)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func writeIgnoreList(path string, names []string) error {
	return atomicWriteFile(path, formatIgnoreList(names), 0o644)
}

func readIgnoreList(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var names []string
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if isGeneratedHeaderLine(trimmed) {
				continue
			}
			return nil, fmt.Errorf("%s:%d: unsupported comment; packages.ignore is generated and only allows the standard header — undo manual edits, or run 'archstate snapshot restore <id>' to recover", path, lineNo)
		}
		if err := validatePackageEntry(trimmed, ""); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
		names = append(names, trimmed)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return uniqueSorted(names), nil
}

func ignoreSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

func filterIgnoredNames(names []string, ignored map[string]bool) []string {
	if len(ignored) == 0 {
		return names
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if ignored[name] {
			continue
		}
		out = append(out, name)
	}
	return out
}

func filterIgnoredState(entries map[string]string, ignored map[string]bool) map[string]string {
	if len(ignored) == 0 {
		return entries
	}
	out := make(map[string]string, len(entries))
	for name, value := range entries {
		if ignored[name] {
			continue
		}
		out[name] = value
	}
	return out
}

func (r *Runner) loadPackageIgnoreSet(repo repoPaths) (map[string]bool, error) {
	names, err := readIgnoreList(repo.packagesIgnorePath())
	if err != nil {
		return nil, err
	}
	return ignoreSet(names), nil
}

func (r *Runner) runIgnore(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		return r.printCommandHelp("ignore")
	}
	if len(args) < 1 {
		return fmt.Errorf("usage: archstate ignore add <pkg>...\n   or: archstate ignore rm <pkg>...\n   or: archstate ignore list")
	}
	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: archstate ignore add <pkg>...")
		}
		return r.runIgnoreAdd(repo, args[1:])
	case "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: archstate ignore rm <pkg>...")
		}
		return r.runIgnoreRemove(repo, args[1:])
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate ignore list")
		}
		return r.runIgnoreList(repo)
	default:
		return fmt.Errorf("unknown ignore command %q; expected add, rm, or list", args[0])
	}
}

func (r *Runner) runIgnoreAdd(repo repoPaths, names []string) error {
	if err := validateNameArgs(names); err != nil {
		return err
	}
	return r.withRepoLock(repo, "ignore add", func() error {
		for _, name := range names {
			if err := validatePackageEntry(name, ""); err != nil {
				return err
			}
		}
		existing, err := readIgnoreList(repo.packagesIgnorePath())
		if err != nil {
			return err
		}
		set := ignoreSet(existing)
		changed := false
		for _, name := range names {
			if set[name] {
				fmt.Fprintf(r.Stdout, "already ignoring %s\n", name)
				continue
			}
			set[name] = true
			changed = true
			fmt.Fprintf(r.Stdout, "ignoring %s\n", name)
		}
		if !changed {
			return nil
		}
		if _, err := r.createAutoSnapshot(repo); err != nil {
			return err
		}
		return writeIgnoreList(repo.packagesIgnorePath(), setKeys(set))
	})
}

func (r *Runner) runIgnoreRemove(repo repoPaths, names []string) error {
	if err := validateNameArgs(names); err != nil {
		return err
	}
	return r.withRepoLock(repo, "ignore rm", func() error {
		for _, name := range names {
			if err := validatePackageEntry(name, ""); err != nil {
				return err
			}
		}
		existing, err := readIgnoreList(repo.packagesIgnorePath())
		if err != nil {
			return err
		}
		set := ignoreSet(existing)
		changed := false
		for _, name := range names {
			if !set[name] {
				fmt.Fprintf(r.Stdout, "%s is not ignored\n", name)
				continue
			}
			delete(set, name)
			changed = true
			fmt.Fprintf(r.Stdout, "stopped ignoring %s\n", name)
		}
		if !changed {
			return nil
		}
		if _, err := r.createAutoSnapshot(repo); err != nil {
			return err
		}
		return writeIgnoreList(repo.packagesIgnorePath(), setKeys(set))
	})
}

func (r *Runner) runIgnoreList(repo repoPaths) error {
	names, err := readIgnoreList(repo.packagesIgnorePath())
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Fprintln(r.Stdout, "no ignored packages")
		return nil
	}
	fmt.Fprintln(r.Stdout, "Ignored packages:")
	for _, name := range names {
		fmt.Fprintf(r.Stdout, "  %s\n", name)
	}
	return nil
}

func setKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
