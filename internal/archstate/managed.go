package archstate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type ManagedActionKind string

const (
	ManagedNoopAction     ManagedActionKind = "noop"
	ManagedSymlinkAction  ManagedActionKind = "symlink"
	ManagedAdoptAction    ManagedActionKind = "adopt"
	ManagedRestoreAction  ManagedActionKind = "restore"
	ManagedConflictAction ManagedActionKind = "conflict"
	ManagedErrorAction    ManagedActionKind = "error"
)

type ManagedAction struct {
	Kind      ManagedActionKind
	Name      string
	Target    string
	LocalPath string
	RepoPath  string
	Message   string
	Err       error
	// ReplacesRepo is set on an adopt action whose tracked copy already exists,
	// so the plan/status output can warn that adopting will discard it.
	ReplacesRepo bool
}

type managedRoot struct {
	Kind      string
	RepoRoot  string
	LocalPath func(string) string
	RepoPath  func(string) string
}

func configRoot(repo repoPaths) managedRoot {
	return managedRoot{
		Kind:      "config",
		RepoRoot:  configDirName,
		LocalPath: repo.localConfig,
		RepoPath:  repo.repoConfig,
	}
}

func homeRoot(repo repoPaths) managedRoot {
	return managedRoot{
		Kind:      "home file",
		RepoRoot:  homeDirName,
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
	cmd := managedCommand(root)
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
		action.Err = fmt.Errorf("cannot read %s %q at %s: %w", root.Kind, name, localPath, localErr)
		return action
	}
	if err := checkRepoTarget(repoInfo, repoErr, repoPath); err != nil {
		action.Kind = ManagedErrorAction
		action.Err = err
		return action
	}

	if localMissing {
		if repoMissing {
			action.Kind = ManagedErrorAction
			action.Err = fmt.Errorf("%s %q is tracked but its saved copy is missing at %s, and nothing exists at %s to link; restore a snapshot, or run 'archstate %s rm %s' to stop tracking it", root.Kind, name, repoPath, localPath, cmd, name)
			return action
		}
		action.Kind = ManagedSymlinkAction
		return action
	}

	localIsSymlink := localInfo.Mode()&os.ModeSymlink != 0
	if localIsSymlink {
		if isCorrectSymlink(localPath, repoPath) {
			if repoMissing {
				action.Kind = ManagedErrorAction
				action.Err = fmt.Errorf("%s %q is a managed symlink but its tracked copy is missing at %s; restore a snapshot, or run 'archstate %s rm %s' to stop tracking it", root.Kind, name, repoPath, cmd, name)
				return action
			}
			action.Kind = ManagedNoopAction
			return action
		}
		if opts.Adopt {
			action.Kind = ManagedErrorAction
			action.Err = adoptSymlinkError(root, name, localPath, repoMissing)
			return action
		}
	}

	if opts.Adopt {
		action.Kind = ManagedAdoptAction
		action.ReplacesRepo = !repoMissing
		return action
	}
	if opts.Restore {
		if repoMissing {
			action.Kind = ManagedErrorAction
			action.Err = fmt.Errorf("cannot restore %s %q: no tracked copy exists yet at %s; use --adopt to save the current %s into Archstate instead", root.Kind, name, repoPath, root.Kind)
			return action
		}
		action.Kind = ManagedRestoreAction
		return action
	}

	action.Kind = ManagedConflictAction
	action.Message = conflictMessage(root, localPath, localIsSymlink, repoMissing)
	return action
}

// adoptSymlinkError explains why --adopt cannot act on a symlink and what to do
// instead, tailored to whether a tracked copy exists to restore.
func adoptSymlinkError(root managedRoot, name, localPath string, repoMissing bool) error {
	tgt := symlinkTargetHint(localPath)
	if repoMissing {
		return fmt.Errorf("cannot adopt %s %q: %s is a symlink%s and no tracked copy exists yet; replace it with a real file or dir, then re-run --adopt", root.Kind, name, localPath, tgt)
	}
	return fmt.Errorf("cannot adopt %s %q: %s is a symlink%s, not a real file; use --restore to install the tracked copy over it, or replace it with a real file or dir to adopt", root.Kind, name, localPath, tgt)
}

// conflictMessage describes an unmanaged-entry conflict and the exact flags that
// resolve it, narrowed to the case at hand so no suggested flag would error.
func conflictMessage(root managedRoot, localPath string, localIsSymlink, repoMissing bool) string {
	switch {
	case localIsSymlink && repoMissing:
		return fmt.Sprintf("path is a symlink%s and no tracked copy exists yet; replace it with a real file or dir, then use --adopt to save it", symlinkTargetHint(localPath))
	case localIsSymlink:
		return fmt.Sprintf("path is a symlink%s, not the tracked copy; use --restore to install the tracked copy over it, or replace it with a real file or dir to adopt it", symlinkTargetHint(localPath))
	case repoMissing:
		return "no tracked copy exists yet; use --adopt to save the current " + root.Kind + " into Archstate"
	default:
		return "use --adopt to save the current " + root.Kind + " into Archstate, or --restore to install the tracked copy over it"
	}
}

// symlinkTargetHint returns " to <target>" for a readable symlink, or "" when the
// link cannot be read, so callers can splice it in without a dangling preposition.
func symlinkTargetHint(localPath string) string {
	target, err := os.Readlink(localPath)
	if err != nil {
		return ""
	}
	return " to " + target
}

func applyManagedAction(action ManagedAction) error {
	switch action.Kind {
	case ManagedNoopAction:
		return nil
	case ManagedSymlinkAction:
		return createConfigSymlink(action.LocalPath, action.RepoPath)
	case ManagedAdoptAction:
		return adoptManagedEntry(action.LocalPath, action.RepoPath, true)
	case ManagedRestoreAction:
		if err := os.RemoveAll(action.LocalPath); err != nil {
			return err
		}
		return createConfigSymlink(action.LocalPath, action.RepoPath)
	case ManagedConflictAction:
		return fmt.Errorf("unmanaged conflict at %s: %s", action.LocalPath, action.Message)
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

func adoptManagedEntry(localPath, repoPath string, replaceRepo bool) error {
	if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
		return err
	}
	if replaceRepo {
		if err := os.RemoveAll(repoPath); err != nil {
			return err
		}
	}
	if err := os.Rename(localPath, repoPath); err != nil {
		return err
	}
	if err := createConfigSymlink(localPath, repoPath); err != nil {
		rollbackAdoptedEntry(localPath, repoPath)
		return err
	}
	return nil
}

func rollbackAdoptedEntry(localPath, repoPath string) {
	if isCorrectSymlink(localPath, repoPath) {
		_ = os.Remove(localPath)
	}
	if !pathExists(localPath) {
		_ = os.Rename(repoPath, localPath)
	}
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

// checkRepoTarget reports a problem with a managed repo target, given the result
// of os.Lstat(repoPath). A missing target is allowed (returns nil), a stat error
// is surfaced, and a symlink is rejected: repo targets must be real files or dirs.
func checkRepoTarget(info os.FileInfo, err error, repoPath string) error {
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("tracked copy at %s must be a real file or dir, not a symlink; remove it from the repo, then re-add the entry", repoPath)
	}
	return nil
}

func (r *Runner) runConfigAdd(repo repoPaths, names []string) error {
	return r.runManagedAdd(repo, configRoot(repo), repo.configPath(), names)
}

func (r *Runner) runHomeAdd(repo repoPaths, names []string) error {
	return r.runManagedAdd(repo, homeRoot(repo), repo.homePath(), names)
}

// validateNameArgs guards the chained name list for add/rm: at least one name,
// nothing that looks like a flag (a likely typo such as `add --restore nvim`),
// and no duplicates within one command.
func validateNameArgs(names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("expected at least one name")
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if strings.HasPrefix(name, "-") {
			return fmt.Errorf("unexpected flag-like argument %q; pass entry names only", name)
		}
		if seen[name] {
			return fmt.Errorf("duplicate name %q", name)
		}
		seen[name] = true
	}
	return nil
}

func (r *Runner) runConfigList(repo repoPaths) error {
	return r.runManagedList(configRoot(repo), repo.configPath())
}

func (r *Runner) runHomeList(repo repoPaths) error {
	return r.runManagedList(homeRoot(repo), repo.homePath())
}

func (r *Runner) runManagedList(root managedRoot, configPath string) error {
	entries, err := readStateFileStrictOptional(configPath, validateManagedEntry)
	if err != nil {
		return err
	}
	label := managedCommand(root)
	if len(entries) == 0 {
		fmt.Fprintf(r.Stdout, "no %s entries tracked\n", label)
		return nil
	}
	fmt.Fprintf(r.Stdout, "Tracked %s entries:\n", label)
	for _, name := range sortedEntryKeys(entries) {
		fmt.Fprintf(r.Stdout, "  %s -> %s/%s\n", name, root.RepoRoot, entries[name])
	}
	return nil
}

func (r *Runner) runConfigPreview(repo repoPaths) error {
	localRoot := filepath.Join(repo.home, ".config")
	exclude := map[string]bool{}
	if filepath.Dir(repo.path) == localRoot {
		exclude[filepath.Base(repo.path)] = true // never offer the archstate repo itself
	}
	return r.runManagedPreview(configRoot(repo), repo.configPath(), localRoot, false, exclude)
}

func (r *Runner) runHomePreview(repo repoPaths) error {
	exclude := map[string]bool{".config": true, ".cache": true, ".local": true}
	return r.runManagedPreview(homeRoot(repo), repo.homePath(), repo.home, true, exclude)
}

// runManagedPreview is the read-only discovery view: it lists the entries under
// localRoot and classifies each as already tracked, addable (a real file/dir),
// or a symlink that can't be adopted as-is. dotfilesOnly limits the scan to
// dotfiles (used for ~, which is otherwise full of non-config entries).
func (r *Runner) runManagedPreview(root managedRoot, configPath, localRoot string, dotfilesOnly bool, exclude map[string]bool) error {
	entries, err := readStateFileStrictOptional(configPath, validateManagedEntry)
	if err != nil {
		return err
	}
	dirEntries, err := os.ReadDir(localRoot)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	label := managedCommand(root)
	fmt.Fprintf(r.Stdout, "%s entries under %s:\n", label, homeRelativePath(r.Home, localRoot))

	shown, addable := 0, 0
	for _, entry := range dirEntries {
		name := entry.Name()
		if exclude[name] || (dotfilesOnly && !strings.HasPrefix(name, ".")) {
			continue
		}
		shown++
		if _, tracked := entries[name]; tracked {
			fmt.Fprintf(r.Stdout, "  %-8s %s\n", "tracked", name)
			continue
		}
		info, err := os.Lstat(root.LocalPath(name))
		if err != nil {
			shown-- // raced away between ReadDir and Lstat
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(r.Stdout, "  %-8s %s  (replace with a real file or dir to add)\n", "symlink", name)
			continue
		}
		fmt.Fprintf(r.Stdout, "  %-8s %s\n", "add", name)
		addable++
	}

	if shown == 0 {
		fmt.Fprintln(r.Stdout, "  none")
		return nil
	}
	if addable > 0 {
		fmt.Fprintf(r.Stdout, "add with: archstate %s add <name>\n", label)
	}
	return nil
}

func (r *Runner) runManagedAdd(repo repoPaths, root managedRoot, configPath string, names []string) error {
	if err := validateNameArgs(names); err != nil {
		return err
	}
	return r.withRepoLock(repo, root.Kind+" add", func() error {
		return r.runManagedAddLocked(repo, root, configPath, names)
	})
}

type managedAddAction int

const (
	managedAddTrack managedAddAction = iota // already the managed symlink, or a repo copy exists: just record it
	managedAddAdopt                         // a real local file/dir with no repo copy: move it in, then symlink
	managedAddNoop                          // neither local nor repo exists: nothing to do
)

type managedAddStep struct {
	name      string
	localPath string
	repoPath  string
	action    managedAddAction
}

func (r *Runner) runManagedAddLocked(repo repoPaths, root managedRoot, configPath string, names []string) error {
	for _, name := range names {
		if err := validateDirectChildName(name); err != nil {
			return fmt.Errorf("invalid %s name %q: %w", root.Kind, name, err)
		}
	}
	entries, err := readStateFileStrictOptional(configPath, validateManagedEntry)
	if err != nil {
		return err
	}

	// Phase 1: classify every name without mutating anything, so a single
	// un-addable entry aborts the whole batch before any file is moved.
	steps := make([]managedAddStep, 0, len(names))
	for _, name := range names {
		step, err := planManagedAdd(root, name)
		if err != nil {
			return err
		}
		steps = append(steps, step)
	}

	// Phase 2: apply. Adoptions move files, so on any failure roll back the
	// adoptions already done this batch and leave the state file unwritten.
	var adopted []managedAddStep
	changed := false
	for _, step := range steps {
		switch step.action {
		case managedAddAdopt:
			if err := adoptManagedEntry(step.localPath, step.repoPath, false); err != nil {
				rollbackManagedAdds(adopted)
				return err
			}
			adopted = append(adopted, step)
			entries[step.name] = step.name
			changed = true
			fmt.Fprintf(r.Stdout, "adopted %s\n", step.name)
		case managedAddTrack:
			entries[step.name] = step.name
			changed = true
			fmt.Fprintf(r.Stdout, "added %s\n", step.name)
		case managedAddNoop:
			fmt.Fprintf(r.Stdout, "nothing to add for %s\n", step.name)
		}
	}
	if changed {
		if err := writeStateFile(configPath, entries); err != nil {
			rollbackManagedAdds(adopted)
			return err
		}
	}
	return nil
}

func planManagedAdd(root managedRoot, name string) (managedAddStep, error) {
	step := managedAddStep{
		name:      name,
		localPath: root.LocalPath(name),
		repoPath:  root.RepoPath(name),
	}
	localInfo, localErr := os.Lstat(step.localPath)
	repoInfo, repoErr := os.Lstat(step.repoPath)
	if localErr != nil && !os.IsNotExist(localErr) {
		return step, localErr
	}
	if err := checkRepoTarget(repoInfo, repoErr, step.repoPath); err != nil {
		return step, err
	}
	localExists := localErr == nil
	repoExists := repoErr == nil
	if localExists {
		if repoExists && isCorrectSymlink(step.localPath, step.repoPath) {
			step.action = managedAddTrack
			return step, nil
		}
		if repoExists {
			return step, fmt.Errorf("cannot add %q: a different tracked copy already exists at %s; run 'archstate bootstrap --adopt' to replace it with the local %s, or 'archstate bootstrap --restore' to install the tracked copy", name, step.repoPath, root.Kind)
		}
		if localInfo.Mode()&os.ModeSymlink != 0 {
			return step, fmt.Errorf("cannot add %q: %s is a symlink%s, not a real file or dir; replace it with a real file or dir first", name, step.localPath, symlinkTargetHint(step.localPath))
		}
		step.action = managedAddAdopt
		return step, nil
	}
	if repoExists {
		step.action = managedAddTrack
		return step, nil
	}
	step.action = managedAddNoop
	return step, nil
}

func rollbackManagedAdds(adopted []managedAddStep) {
	for i := len(adopted) - 1; i >= 0; i-- {
		rollbackAdoptedEntry(adopted[i].localPath, adopted[i].repoPath)
	}
}

func (r *Runner) runConfigRemove(repo repoPaths, names []string) error {
	return r.runManagedRemove(repo, configRoot(repo), repo.configPath(), names)
}

func (r *Runner) runHomeRemove(repo repoPaths, names []string) error {
	return r.runManagedRemove(repo, homeRoot(repo), repo.homePath(), names)
}

func (r *Runner) runManagedRemove(repo repoPaths, root managedRoot, configPath string, names []string) error {
	if err := validateNameArgs(names); err != nil {
		return err
	}
	return r.withRepoLock(repo, root.Kind+" rm", func() error {
		return r.runManagedRemoveLocked(repo, root, configPath, names)
	})
}

type managedRemoveStep struct {
	name      string
	localPath string
	repoPath  string
}

func (r *Runner) runManagedRemoveLocked(repo repoPaths, root managedRoot, configPath string, names []string) error {
	for _, name := range names {
		if err := validateDirectChildName(name); err != nil {
			return fmt.Errorf("invalid %s name %q: %w", root.Kind, name, err)
		}
	}
	entries, err := readStateFileStrictOptional(configPath, validateManagedEntry)
	if err != nil {
		return err
	}

	// Phase 1: classify. Untracked names are reported and skipped; a symlink
	// repo target is a hard error that aborts before any snapshot or mutation.
	steps := make([]managedRemoveStep, 0, len(names))
	for _, name := range names {
		target, ok := entries[name]
		if !ok {
			fmt.Fprintf(r.Stdout, "%s is not tracked\n", name)
			continue
		}
		repoPath := root.RepoPath(target)
		repoInfo, repoErr := os.Lstat(repoPath)
		if err := checkRepoTarget(repoInfo, repoErr, repoPath); err != nil {
			return err
		}
		steps = append(steps, managedRemoveStep{name: name, localPath: root.LocalPath(name), repoPath: repoPath})
	}
	if len(steps) == 0 {
		return nil
	}

	// Phase 2: one snapshot for the batch, commit the state-file change, then
	// the filesystem cleanup. Committing state before mutating files means
	// config.conf never references a target that has already been removed.
	if _, err := r.createAutoSnapshot(repo); err != nil {
		return err
	}
	for _, step := range steps {
		delete(entries, step.name)
	}
	if err := writeStateFile(configPath, entries); err != nil {
		return err
	}
	for _, step := range steps {
		if isCorrectSymlink(step.localPath, step.repoPath) {
			if pathExists(step.repoPath) {
				if err := restoreManagedSymlink(step.localPath, step.repoPath); err != nil {
					return err
				}
			} else if err := os.Remove(step.localPath); err != nil {
				return err
			}
		}
		if err := os.RemoveAll(step.repoPath); err != nil {
			return err
		}
		fmt.Fprintf(r.Stdout, "removed %s\n", step.name)
	}
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

// printManagedSection writes a titled list of managed actions, delegating
// per-action formatting to format. It centralizes the title/empty/loop
// scaffolding shared by the bootstrap-plan and status views.
func printManagedSection(w io.Writer, title, empty string, actions []ManagedAction, format func(io.Writer, ManagedAction)) {
	fmt.Fprintln(w, title)
	if len(actions) == 0 {
		fmt.Fprintf(w, "  %s\n", empty)
		return
	}
	for _, action := range actions {
		format(w, action)
	}
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
