package archstate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type ManagedActionKind string

const (
	ManagedNoopAction      ManagedActionKind = "noop"
	ManagedSymlinkAction   ManagedActionKind = "symlink"
	ManagedAdoptAction     ManagedActionKind = "adopt"
	ManagedOverwriteAction ManagedActionKind = "overwrite"
	ManagedConflictAction  ManagedActionKind = "conflict"
	ManagedErrorAction     ManagedActionKind = "error"
)

type ManagedAction struct {
	Kind      ManagedActionKind
	Name      string
	Target    string
	LocalPath string
	RepoPath  string
	Message   string
	Err       error
}

type ManagedConflict struct {
	Action ManagedAction
}

type managedRoot struct {
	Kind      string
	Config    string
	LocalRoot string
	RepoRoot  string
	LocalPath func(string) string
	RepoPath  func(string) string
}

func configRoot(repo repoPaths) managedRoot {
	return managedRoot{
		Kind:      "config",
		Config:    "config.conf",
		LocalRoot: "~/.config",
		RepoRoot:  "config",
		LocalPath: repo.localConfig,
		RepoPath:  repo.repoConfig,
	}
}

func homeRoot(repo repoPaths) managedRoot {
	return managedRoot{
		Kind:      "home file",
		Config:    "home.conf",
		LocalRoot: "~",
		RepoRoot:  "home",
		LocalPath: repo.localHome,
		RepoPath:  repo.repoHome,
	}
}

func planConfigs(repo repoPaths, entries map[string]string, opts BootstrapOptions) []ManagedAction {
	return planManagedEntries(configRoot(repo), entries, opts)
}

func planHomeFiles(repo repoPaths, entries map[string]string, opts BootstrapOptions) []ManagedAction {
	return planManagedEntries(homeRoot(repo), entries, opts)
}

func planManagedEntries(root managedRoot, entries map[string]string, opts BootstrapOptions) []ManagedAction {
	actions := make([]ManagedAction, 0, len(entries))
	for _, name := range sortedEntryKeys(entries) {
		target := entries[name]
		action := planManagedEntry(root, name, target, opts)
		actions = append(actions, action)
	}
	return actions
}

func planConfig(repo repoPaths, name, target string, opts BootstrapOptions) ManagedAction {
	return planManagedEntry(configRoot(repo), name, target, opts)
}

func planHomeFile(repo repoPaths, name, target string, opts BootstrapOptions) ManagedAction {
	return planManagedEntry(homeRoot(repo), name, target, opts)
}

func planManagedEntry(root managedRoot, name, target string, opts BootstrapOptions) ManagedAction {
	localPath := root.LocalPath(name)
	repoPath := root.RepoPath(target)
	action := ManagedAction{
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
		action.Kind = ManagedErrorAction
		action.Err = localErr
		return action
	}
	if repoErr != nil && !repoMissing {
		action.Kind = ManagedErrorAction
		action.Err = repoErr
		return action
	}

	if localMissing {
		if repoMissing {
			action.Kind = ManagedErrorAction
			action.Err = fmt.Errorf("repo target is missing: %s", repoPath)
			return action
		}
		if repoInfo.Mode()&os.ModeSymlink != 0 {
			action.Kind = ManagedErrorAction
			action.Err = fmt.Errorf("repo target must not be a symlink: %s", repoPath)
			return action
		}
		action.Kind = ManagedSymlinkAction
		return action
	}

	if localInfo.Mode()&os.ModeSymlink != 0 {
		if isCorrectSymlink(localPath, repoPath) {
			action.Kind = ManagedNoopAction
			return action
		}
	}

	if opts.Adopt {
		action.Kind = ManagedAdoptAction
		return action
	}
	if opts.Overwrite {
		if repoMissing {
			action.Kind = ManagedErrorAction
			action.Err = fmt.Errorf("cannot overwrite %s: no tracked copy exists at %s; use --adopt to save the current config into Archstate", localPath, repoPath)
			return action
		}
		action.Kind = ManagedOverwriteAction
		return action
	}

	action.Kind = ManagedConflictAction
	if repoMissing {
		action.Message = "no tracked copy exists; use --adopt to save the current " + root.Kind + " into Archstate"
	} else {
		action.Message = "use --adopt to save the current " + root.Kind + " into Archstate, or --overwrite to restore the tracked copy"
	}
	return action
}

func applyManagedAction(action ManagedAction) error {
	switch action.Kind {
	case ManagedNoopAction:
		return nil
	case ManagedSymlinkAction:
		return createConfigSymlink(action.LocalPath, action.RepoPath)
	case ManagedAdoptAction:
		if err := os.MkdirAll(filepath.Dir(action.RepoPath), 0o755); err != nil {
			return err
		}
		if err := os.RemoveAll(action.RepoPath); err != nil {
			return err
		}
		if err := os.Rename(action.LocalPath, action.RepoPath); err != nil {
			return err
		}
		return createConfigSymlink(action.LocalPath, action.RepoPath)
	case ManagedOverwriteAction:
		if err := os.RemoveAll(action.LocalPath); err != nil {
			return err
		}
		return createConfigSymlink(action.LocalPath, action.RepoPath)
	case ManagedConflictAction:
		return fmt.Errorf("unmanaged config conflict at %s", action.LocalPath)
	case ManagedErrorAction:
		return action.Err
	default:
		return fmt.Errorf("unknown managed action %q", action.Kind)
	}
}

func createConfigSymlink(localPath, repoPath string) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	return os.Symlink(repoPath, localPath)
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

func (r *Runner) runConfigAdd(repo repoPaths, name string) error {
	return r.runManagedAdd(repo, configRoot(repo), repo.configPath(), name, false)
}

func (r *Runner) runHomeAdd(repo repoPaths, name string) error {
	return r.runManagedAdd(repo, homeRoot(repo), repo.homePath(), name, true)
}

func (r *Runner) runManagedAdd(repo repoPaths, root managedRoot, configPath, name string, optionalConfig bool) error {
	if err := validateDirectChildName(name); err != nil {
		return fmt.Errorf("invalid %s name %q: %w", root.Kind, name, err)
	}
	var entries map[string]string
	var err error
	if optionalConfig {
		entries, err = readStateFileStrictOptional(configPath, validateManagedEntry)
	} else {
		entries, err = readStateFileStrict(configPath, validateManagedEntry)
	}
	if err != nil {
		return err
	}
	localPath := root.LocalPath(name)
	repoPath := root.RepoPath(name)
	localExists := pathExists(localPath)
	repoExists := pathExists(repoPath)

	if localExists {
		if repoExists && isCorrectSymlink(localPath, repoPath) {
			entries[name] = name
			if err := writeStateFile(configPath, entries); err != nil {
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
		if err := writeStateFile(configPath, entries); err != nil {
			return err
		}
		fmt.Fprintf(r.Stdout, "adopted %s\n", name)
		return nil
	}

	if repoExists {
		entries[name] = name
		if err := writeStateFile(configPath, entries); err != nil {
			return err
		}
		fmt.Fprintf(r.Stdout, "added %s\n", name)
		return nil
	}

	fmt.Fprintf(r.Stdout, "nothing to add for %s\n", name)
	return nil
}

func (r *Runner) runConfigRemove(repo repoPaths, name string) error {
	return r.runManagedRemove(repo, configRoot(repo), repo.configPath(), name, false)
}

func (r *Runner) runHomeRemove(repo repoPaths, name string) error {
	return r.runManagedRemove(repo, homeRoot(repo), repo.homePath(), name, true)
}

func (r *Runner) runManagedRemove(repo repoPaths, root managedRoot, configPath, name string, optionalConfig bool) error {
	if err := validateDirectChildName(name); err != nil {
		return fmt.Errorf("invalid %s name %q: %w", root.Kind, name, err)
	}
	var entries map[string]string
	var err error
	if optionalConfig {
		entries, err = readStateFileStrictOptional(configPath, validateManagedEntry)
	} else {
		entries, err = readStateFileStrict(configPath, validateManagedEntry)
	}
	if err != nil {
		return err
	}
	target, ok := entries[name]
	if !ok {
		fmt.Fprintf(r.Stdout, "%s is not tracked\n", name)
		return nil
	}
	if _, err := r.createAutoSnapshot(repo); err != nil {
		return err
	}
	delete(entries, name)
	localPath := root.LocalPath(name)
	repoPath := root.RepoPath(target)
	if isCorrectSymlink(localPath, repoPath) {
		if pathExists(repoPath) {
			if err := restoreManagedSymlink(localPath, repoPath); err != nil {
				return err
			}
		} else if err := os.Remove(localPath); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(repoPath); err != nil {
		return err
	}
	if err := writeStateFile(configPath, entries); err != nil {
		return err
	}
	fmt.Fprintf(r.Stdout, "removed %s\n", name)
	return nil
}

func restoreManagedSymlink(localPath, repoPath string) error {
	tmpDir, err := os.MkdirTemp(filepath.Dir(localPath), "."+filepath.Base(localPath)+".archstate-restore-")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	tmpPath := filepath.Join(tmpDir, "entry")
	if err := copyPath(repoPath, tmpPath); err != nil {
		return err
	}
	if err := os.Remove(localPath); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, localPath); err != nil {
		return err
	}
	cleanup = false
	return os.RemoveAll(tmpDir)
}

func copyPath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if info.IsDir() {
		return copyDir(src, dst, info.Mode())
	}
	if info.Mode().IsRegular() {
		return copyFile(src, dst, info.Mode())
	}
	return fmt.Errorf("unsupported file type: %s", src)
}

func copyDir(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(dst, mode.Perm()); err != nil {
		return err
	}
	if err := os.Chmod(dst, mode.Perm()); err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == src {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, rel)
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(target, dstPath)
		}
		if info.IsDir() {
			if err := os.MkdirAll(dstPath, info.Mode().Perm()); err != nil {
				return err
			}
			return os.Chmod(dstPath, info.Mode().Perm())
		}
		if info.Mode().IsRegular() {
			return copyFile(path, dstPath, info.Mode())
		}
		return fmt.Errorf("unsupported file type: %s", path)
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		_ = out.Close()
		if cleanup {
			_ = os.Remove(dst)
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Chmod(mode.Perm()); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func checkConfigHealth(repo repoPaths, entries map[string]string) error {
	return checkManagedHealth(repo, configRoot(repo), entries)
}

func checkManagedHealth(repo repoPaths, root managedRoot, entries map[string]string) error {
	for _, name := range sortedEntryKeys(entries) {
		target := entries[name]
		action := planManagedEntry(root, name, target, BootstrapOptions{})
		switch action.Kind {
		case ManagedNoopAction:
			continue
		case ManagedSymlinkAction:
			return fmt.Errorf("managed symlink is missing: %s -> %s", action.LocalPath, action.RepoPath)
		case ManagedConflictAction:
			return fmt.Errorf("unmanaged config conflict at %s", action.LocalPath)
		case ManagedErrorAction:
			return action.Err
		default:
			continue
		}
	}
	return nil
}
