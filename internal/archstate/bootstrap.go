package archstate

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

type BootstrapOptions struct {
	Preview   bool
	Adopt     bool
	Overwrite bool
	AURHelper string
}

type BootstrapPlan struct {
	Repo               repoPaths
	NativeMissing      []string
	AURMissing         []string
	AURHelper          string
	AURHelperPath      string
	AURHelperPackage   string
	AURHelperBootstrap bool
	AURHelperError     error
	ConfigActions      []ManagedAction
	HomeActions        []ManagedAction
	ManagedErrors      []error
}

func (r *Runner) buildBootstrapPlan(repo repoPaths, opts BootstrapOptions) (BootstrapPlan, error) {
	nativeState, err := readStateFileStrictOptional(repo.pacmanPath(), validatePackageEntry)
	if err != nil {
		return BootstrapPlan{}, err
	}
	aurState, err := readStateFileStrictOptional(repo.aurPath(), validatePackageEntry)
	if err != nil {
		return BootstrapPlan{}, err
	}
	configState, err := readStateFileStrictOptional(repo.configPath(), validateManagedEntry)
	if err != nil {
		return BootstrapPlan{}, err
	}
	homeState, err := readStateFileStrictOptional(repo.homePath(), validateManagedEntry)
	if err != nil {
		return BootstrapPlan{}, err
	}
	installed, err := r.queryPackageNames("pacman", "-Qq")
	if err != nil {
		return BootstrapPlan{}, err
	}

	plan := BootstrapPlan{
		Repo:          repo,
		NativeMissing: missingPackages(nativeState, installed),
		AURMissing:    missingPackages(aurState, installed),
		ConfigActions: planConfigs(repo, configState, opts),
		HomeActions:   planHomeFiles(repo, homeState, opts),
	}
	if len(plan.AURMissing) > 0 {
		helper, helperPath, needsBootstrap, err := r.resolveAURHelper(opts.AURHelper)
		if err != nil {
			plan.AURHelperError = err
		} else {
			plan.AURHelper = helper
			plan.AURHelperPath = helperPath
			plan.AURHelperPackage = aurHelperPackage(helper)
			plan.AURHelperBootstrap = needsBootstrap
		}
	}
	for _, action := range plan.allManagedActions() {
		if action.Kind == ManagedErrorAction {
			plan.ManagedErrors = append(plan.ManagedErrors, action.Err)
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
			if plan.AURHelperBootstrap {
				fmt.Fprintf(r.Stdout, "  AUR helper bootstrap: %s\n", plan.AURHelperPackage)
			}
		}
	}

	printManagedPlan(r.Stdout, "Config plan:", "no config entries declared", plan.ConfigActions)
	printManagedPlan(r.Stdout, "Home file plan:", "no home files declared", plan.HomeActions)
}

func printManagedPlan(w io.Writer, title, empty string, actions []ManagedAction) {
	printManagedSection(w, title, empty, actions, func(w io.Writer, action ManagedAction) {
		switch action.Kind {
		case ManagedNoopAction:
			fmt.Fprintf(w, "  ok %s\n", action.LocalPath)
		case ManagedSymlinkAction:
			fmt.Fprintf(w, "  link %s -> %s\n", action.LocalPath, action.RepoPath)
		case ManagedAdoptAction:
			fmt.Fprintf(w, "  adopt %s -> %s\n", action.LocalPath, action.RepoPath)
		case ManagedOverwriteAction:
			fmt.Fprintf(w, "  overwrite %s -> %s\n", action.RepoPath, action.LocalPath)
		case ManagedConflictAction:
			fmt.Fprintf(w, "  conflict %s: %s\n", action.LocalPath, action.Message)
		case ManagedErrorAction:
			fmt.Fprintf(w, "  error %s: %v\n", action.LocalPath, action.Err)
		}
	})
}

func printPackageList(w io.Writer, label string, names []string) {
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
	if len(plan.ManagedErrors) > 0 {
		return plan.ManagedErrors[0]
	}

	for _, action := range plan.allManagedActions() {
		if action.Kind == ManagedConflictAction {
			return fmt.Errorf("unmanaged config conflict at %s: %s", action.LocalPath, action.Message)
		}
	}
	if plan.hasRiskyManagedActions() {
		if _, err := r.createAutoSnapshot(plan.Repo); err != nil {
			return err
		}
	}

	if len(plan.NativeMissing) > 0 {
		args := append([]string{"pacman", "-S", "--needed"}, plan.NativeMissing...)
		if err := r.streamCommand("sudo", args...); err != nil {
			return err
		}
	}
	if len(plan.AURMissing) > 0 {
		if plan.AURHelperBootstrap {
			helperPath, err := r.bootstrapAURHelper(plan.AURHelper)
			if err != nil {
				return err
			}
			plan.AURHelperPath = helperPath
		}
		args := append([]string{"-S", "--needed"}, plan.AURMissing...)
		if err := r.streamCommand(plan.AURHelperPath, args...); err != nil {
			return err
		}
	}
	for _, action := range plan.allManagedActions() {
		if err := applyManagedAction(action); err != nil {
			return err
		}
	}
	return nil
}

func (p BootstrapPlan) hasRiskyManagedActions() bool {
	for _, action := range p.allManagedActions() {
		switch action.Kind {
		case ManagedAdoptAction, ManagedOverwriteAction:
			return true
		}
	}
	return false
}

func (p BootstrapPlan) allManagedActions() []ManagedAction {
	actions := make([]ManagedAction, 0, len(p.ConfigActions)+len(p.HomeActions))
	actions = append(actions, p.ConfigActions...)
	actions = append(actions, p.HomeActions...)
	return actions
}

func sortedEntryKeys(entries map[string]string) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
