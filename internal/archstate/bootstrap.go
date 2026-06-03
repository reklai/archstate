package archstate

import (
	"fmt"
	"sort"
	"strings"
)

type BootstrapOptions struct {
	Preview   bool
	Adopt     bool
	Overwrite bool
}

type BootstrapPlan struct {
	Repo             repoPaths
	NativeMissing    []string
	AURMissing       []string
	AURHelper        string
	AURHelperError   error
	DotfileActions   []DotfileAction
	DotfileConflicts []DotfileConflict
	DotfileErrors    []error
}

func (r *Runner) buildBootstrapPlan(repo repoPaths, opts BootstrapOptions) (BootstrapPlan, error) {
	nativeState, err := readStateFileStrict(repo.pacmanPath(), validatePackageEntry)
	if err != nil {
		return BootstrapPlan{}, err
	}
	aurState, err := readStateFileStrict(repo.aurPath(), validatePackageEntry)
	if err != nil {
		return BootstrapPlan{}, err
	}
	dotState, err := readStateFileStrict(repo.dotfilesPath(), validateDotfileEntry)
	if err != nil {
		return BootstrapPlan{}, err
	}
	installed, err := r.queryPackageNames("pacman", "-Qq")
	if err != nil {
		return BootstrapPlan{}, err
	}

	plan := BootstrapPlan{
		Repo:           repo,
		NativeMissing:  missingPackages(nativeState, installed),
		AURMissing:     missingPackages(aurState, installed),
		DotfileActions: planDotfiles(repo, dotState, opts),
	}
	if len(plan.AURMissing) > 0 {
		helper, err := r.findAURHelper()
		if err != nil {
			plan.AURHelperError = err
		} else {
			plan.AURHelper = helper
		}
	}
	for _, action := range plan.DotfileActions {
		switch action.Kind {
		case DotfileConflictAction:
			plan.DotfileConflicts = append(plan.DotfileConflicts, DotfileConflict{Action: action})
		case DotfileErrorAction:
			plan.DotfileErrors = append(plan.DotfileErrors, action.Err)
		}
	}
	return plan, nil
}

func (r *Runner) printBootstrapPlan(plan BootstrapPlan) {
	fmt.Fprintln(r.Stdout, "Package plan:")
	printPackageList(r.Stdout, "native install", plan.NativeMissing)
	printPackageList(r.Stdout, "AUR install", plan.AURMissing)
	if len(plan.AURMissing) > 0 {
		if plan.AURHelperError != nil {
			fmt.Fprintf(r.Stdout, "  AUR helper error: %v\n", plan.AURHelperError)
		} else {
			fmt.Fprintf(r.Stdout, "  AUR helper: %s\n", plan.AURHelper)
		}
	}

	fmt.Fprintln(r.Stdout, "Dotfile plan:")
	if len(plan.DotfileActions) == 0 {
		fmt.Fprintln(r.Stdout, "  no dotfiles declared")
		return
	}
	for _, action := range plan.DotfileActions {
		switch action.Kind {
		case DotfileNoopAction:
			fmt.Fprintf(r.Stdout, "  ok %s\n", action.LocalPath)
		case DotfileSymlinkAction:
			fmt.Fprintf(r.Stdout, "  link %s -> %s\n", action.LocalPath, action.RepoPath)
		case DotfileAdoptAction:
			fmt.Fprintf(r.Stdout, "  adopt %s -> %s\n", action.LocalPath, action.RepoPath)
		case DotfileOverwriteAction:
			fmt.Fprintf(r.Stdout, "  overwrite %s -> %s\n", action.LocalPath, action.RepoPath)
		case DotfileConflictAction:
			fmt.Fprintf(r.Stdout, "  prompt %s: %s\n", action.LocalPath, action.Message)
		case DotfileErrorAction:
			fmt.Fprintf(r.Stdout, "  error %s: %v\n", action.LocalPath, action.Err)
		}
	}
}

func printPackageList(w interface{ Write([]byte) (int, error) }, label string, names []string) {
	if len(names) == 0 {
		fmt.Fprintf(w, "  %s: none\n", label)
		return
	}
	fmt.Fprintf(w, "  %s: %s\n", label, strings.Join(names, " "))
}

func (r *Runner) applyBootstrapPlan(plan BootstrapPlan, opts BootstrapOptions) error {
	if len(plan.AURMissing) > 0 {
		if plan.AURHelperError != nil {
			return plan.AURHelperError
		}
	}
	if len(plan.DotfileErrors) > 0 {
		return plan.DotfileErrors[0]
	}

	resolvedActions := make([]DotfileAction, 0, len(plan.DotfileActions))
	for _, action := range plan.DotfileActions {
		if action.Kind == DotfileConflictAction {
			resolved, err := r.resolveDotfileConflict(action, opts)
			if err != nil {
				return err
			}
			action = resolved
		}
		resolvedActions = append(resolvedActions, action)
	}

	if len(plan.NativeMissing) > 0 {
		args := append([]string{"pacman", "-S", "--needed"}, plan.NativeMissing...)
		if err := r.streamCommand("sudo", args...); err != nil {
			return err
		}
	}
	if len(plan.AURMissing) > 0 {
		args := append([]string{"-S", "--needed"}, plan.AURMissing...)
		if err := r.streamCommand(plan.AURHelper, args...); err != nil {
			return err
		}
	}
	for _, action := range resolvedActions {
		if err := applyDotfileAction(action); err != nil {
			return err
		}
	}
	return nil
}

func sortedEntryKeys(entries map[string]string) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
