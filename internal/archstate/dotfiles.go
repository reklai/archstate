package archstate

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type DotfileActionKind string

const (
	DotfileNoopAction      DotfileActionKind = "noop"
	DotfileSymlinkAction   DotfileActionKind = "symlink"
	DotfileAdoptAction     DotfileActionKind = "adopt"
	DotfileOverwriteAction DotfileActionKind = "overwrite"
	DotfileConflictAction  DotfileActionKind = "conflict"
	DotfileErrorAction     DotfileActionKind = "error"
)

type DotfileAction struct {
	Kind      DotfileActionKind
	Name      string
	Target    string
	LocalPath string
	RepoPath  string
	Message   string
	Err       error
}

type DotfileConflict struct {
	Action DotfileAction
}

func planDotfiles(repo repoPaths, entries map[string]string, opts BootstrapOptions) []DotfileAction {
	actions := make([]DotfileAction, 0, len(entries))
	for _, name := range sortedEntryKeys(entries) {
		target := entries[name]
		action := planDotfile(repo, name, target, opts)
		actions = append(actions, action)
	}
	return actions
}

func planDotfile(repo repoPaths, name, target string, opts BootstrapOptions) DotfileAction {
	localPath := repo.localConfig(name)
	repoPath := repo.repoDotfile(target)
	action := DotfileAction{
		Name:      name,
		Target:    target,
		LocalPath: localPath,
		RepoPath:  repoPath,
	}
	localInfo, localErr := os.Lstat(localPath)
	repoInfo, repoErr := os.Lstat(repoPath)
	localMissing := os.IsNotExist(localErr)
	repoMissing := os.IsNotExist(repoErr)
	if localErr != nil && !localMissing {
		action.Kind = DotfileErrorAction
		action.Err = localErr
		return action
	}
	if repoErr != nil && !repoMissing {
		action.Kind = DotfileErrorAction
		action.Err = repoErr
		return action
	}

	if localMissing {
		if repoMissing {
			action.Kind = DotfileErrorAction
			action.Err = fmt.Errorf("repo target is missing: %s", repoPath)
			return action
		}
		if repoInfo.Mode()&os.ModeSymlink != 0 {
			action.Kind = DotfileErrorAction
			action.Err = fmt.Errorf("repo target must not be a symlink: %s", repoPath)
			return action
		}
		action.Kind = DotfileSymlinkAction
		return action
	}

	if localInfo.Mode()&os.ModeSymlink != 0 {
		if isCorrectSymlink(localPath, repoPath) {
			action.Kind = DotfileNoopAction
			return action
		}
	}

	if opts.Adopt {
		if !repoMissing {
			action.Kind = DotfileErrorAction
			action.Err = fmt.Errorf("cannot adopt %s because repo target already exists: %s", localPath, repoPath)
			return action
		}
		action.Kind = DotfileAdoptAction
		return action
	}
	if opts.Overwrite {
		if repoMissing {
			action.Kind = DotfileErrorAction
			action.Err = fmt.Errorf("cannot overwrite %s because repo target is missing: %s", localPath, repoPath)
			return action
		}
		action.Kind = DotfileOverwriteAction
		return action
	}

	action.Kind = DotfileConflictAction
	if repoMissing {
		action.Message = "unmanaged local config exists and repo target is missing; adopt is available"
	} else {
		action.Message = "unmanaged local config exists; overwrite is available"
	}
	return action
}

func applyDotfileAction(action DotfileAction) error {
	switch action.Kind {
	case DotfileNoopAction:
		return nil
	case DotfileSymlinkAction:
		return createConfigSymlink(action.LocalPath, action.RepoPath)
	case DotfileAdoptAction:
		if err := os.MkdirAll(filepath.Dir(action.RepoPath), 0o755); err != nil {
			return err
		}
		if err := os.Rename(action.LocalPath, action.RepoPath); err != nil {
			return err
		}
		return createConfigSymlink(action.LocalPath, action.RepoPath)
	case DotfileOverwriteAction:
		if err := os.RemoveAll(action.LocalPath); err != nil {
			return err
		}
		return createConfigSymlink(action.LocalPath, action.RepoPath)
	case DotfileConflictAction:
		return fmt.Errorf("unmanaged dotfile conflict at %s", action.LocalPath)
	case DotfileErrorAction:
		return action.Err
	default:
		return fmt.Errorf("unknown dotfile action %q", action.Kind)
	}
}

func createConfigSymlink(localPath, repoPath string) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	return os.Symlink(repoPath, localPath)
}

func (r *Runner) resolveDotfileConflict(action DotfileAction, opts BootstrapOptions) (DotfileAction, error) {
	if opts.Adopt || opts.Overwrite {
		return action, nil
	}
	if !r.isInteractive() {
		return action, fmt.Errorf("unmanaged dotfile conflict at %s; rerun interactively or use --adopt/--overwrite", action.LocalPath)
	}
	repoExists := pathExists(action.RepoPath)
	if repoExists {
		answer, err := r.prompt(fmt.Sprintf("Overwrite unmanaged %s with repo target %s? [y/N] ", action.LocalPath, action.RepoPath))
		if err != nil {
			return action, err
		}
		if isYes(answer) {
			action.Kind = DotfileOverwriteAction
			return action, nil
		}
		return action, fmt.Errorf("unmanaged dotfile conflict at %s", action.LocalPath)
	}
	answer, err := r.prompt(fmt.Sprintf("Adopt unmanaged %s into %s? [y/N] ", action.LocalPath, action.RepoPath))
	if err != nil {
		return action, err
	}
	if isYes(answer) {
		action.Kind = DotfileAdoptAction
		return action, nil
	}
	return action, fmt.Errorf("unmanaged dotfile conflict at %s", action.LocalPath)
}

func (r *Runner) prompt(prompt string) (string, error) {
	fmt.Fprint(r.Stdout, prompt)
	reader := bufio.NewReader(r.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, os.ErrClosed) {
		if len(answer) == 0 {
			return "", err
		}
	}
	return strings.TrimSpace(answer), nil
}

func isYes(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func (r *Runner) isInteractive() bool {
	file, ok := r.Stdin.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func isCorrectSymlink(localPath, repoPath string) bool {
	target, err := os.Readlink(localPath)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(localPath), target)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	repoAbs, err := filepath.Abs(repoPath)
	if err != nil {
		return false
	}
	return filepath.Clean(targetAbs) == filepath.Clean(repoAbs)
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func (r *Runner) runDotAdd(repo repoPaths, name string) error {
	if err := validateDirectChildName(name); err != nil {
		return fmt.Errorf("invalid dotfile name %q: %w", name, err)
	}
	entries, err := readStateFileStrict(repo.dotfilesPath(), validateDotfileEntry)
	if err != nil {
		return err
	}
	localPath := repo.localConfig(name)
	repoPath := repo.repoDotfile(name)
	localExists := pathExists(localPath)
	repoExists := pathExists(repoPath)

	if localExists {
		if repoExists && isCorrectSymlink(localPath, repoPath) {
			entries[name] = name
			if err := writeStateFile(repo.dotfilesPath(), entries); err != nil {
				return err
			}
			fmt.Fprintf(r.Stdout, "added %s\n", name)
			return nil
		}
		if repoExists {
			return fmt.Errorf("cannot adopt %s because repo target already exists: %s", localPath, repoPath)
		}
		if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
			return err
		}
		if err := os.Rename(localPath, repoPath); err != nil {
			return err
		}
		if err := createConfigSymlink(localPath, repoPath); err != nil {
			return err
		}
		entries[name] = name
		if err := writeStateFile(repo.dotfilesPath(), entries); err != nil {
			return err
		}
		fmt.Fprintf(r.Stdout, "adopted %s\n", name)
		return nil
	}

	if repoExists {
		entries[name] = name
		if err := writeStateFile(repo.dotfilesPath(), entries); err != nil {
			return err
		}
		fmt.Fprintf(r.Stdout, "added %s\n", name)
		return nil
	}

	fmt.Fprintf(r.Stdout, "nothing to add for %s\n", name)
	return nil
}

func (r *Runner) runDotRemove(repo repoPaths, name string) error {
	if err := validateDirectChildName(name); err != nil {
		return fmt.Errorf("invalid dotfile name %q: %w", name, err)
	}
	entries, err := readStateFileStrict(repo.dotfilesPath(), validateDotfileEntry)
	if err != nil {
		return err
	}
	target, ok := entries[name]
	if !ok {
		fmt.Fprintf(r.Stdout, "%s is not tracked\n", name)
		return nil
	}
	delete(entries, name)
	localPath := repo.localConfig(name)
	repoPath := repo.repoDotfile(target)
	if isCorrectSymlink(localPath, repoPath) {
		if err := os.Remove(localPath); err != nil {
			return err
		}
	}
	if err := writeStateFile(repo.dotfilesPath(), entries); err != nil {
		return err
	}
	fmt.Fprintf(r.Stdout, "removed %s\n", name)
	return nil
}

func checkDotfileHealth(repo repoPaths, entries map[string]string) error {
	for _, name := range sortedEntryKeys(entries) {
		target := entries[name]
		action := planDotfile(repo, name, target, BootstrapOptions{})
		switch action.Kind {
		case DotfileNoopAction:
			continue
		case DotfileSymlinkAction:
			return fmt.Errorf("managed symlink is missing: %s -> %s", action.LocalPath, action.RepoPath)
		case DotfileConflictAction:
			return fmt.Errorf("unmanaged dotfile conflict at %s", action.LocalPath)
		case DotfileErrorAction:
			return action.Err
		default:
			continue
		}
	}
	return nil
}
