package archstate

import (
	"fmt"
	"io"
)

type PackageDrift struct {
	Missing   []string
	Untracked []string
}

// machineDrift is the shared read-only view of tracked intent vs the current
// machine. status prints it; verify turns it into an exit code.
type machineDrift struct {
	Native PackageDrift
	AUR    PackageDrift
	Config []ManagedAction
	Home   []ManagedAction
}

// driftLayers selects which intent/machine layers computeMachineDrift loads.
// Scope flags on verify/check --exit must skip excluded layers entirely so a
// failure in an out-of-scope layer (e.g. missing pacman during --dotfiles-only)
// cannot abort the gate.
type driftLayers struct {
	Packages bool
	Managed  bool
}

func allDriftLayers() driftLayers {
	return driftLayers{Packages: true, Managed: true}
}

func packageDriftLayers() driftLayers {
	return driftLayers{Packages: true}
}

func driftLayersForVerify(opts VerifyOptions) driftLayers {
	return driftLayers{
		Packages: !opts.DotFilesOnly,
		Managed:  !opts.PackagesOnly,
	}
}

func (r *Runner) computeMachineDrift(repo repoPaths) (machineDrift, error) {
	return r.computeMachineDriftLayers(repo, allDriftLayers())
}

func (r *Runner) computeMachineDriftLayers(repo repoPaths, layers driftLayers) (machineDrift, error) {
	var d machineDrift

	if layers.Packages {
		ignored, err := r.loadPackageIgnoreSet(repo)
		if err != nil {
			return d, err
		}
		nativeState, err := readStateFileStrictOptional(repo.pacmanPath(), validatePackageEntry)
		if err != nil {
			return d, err
		}
		aurState, err := readStateFileStrictOptional(repo.aurPath(), validatePackageEntry)
		if err != nil {
			return d, err
		}
		// Tracked state still lists ignored packages if they were committed before
		// ignore; treat them as non-intent for drift and bootstrap.
		nativeState = filterIgnoredState(nativeState, ignored)
		aurState = filterIgnoredState(aurState, ignored)

		nativeInstalled, err := r.queryPackageNames("pacman", "-Qqen")
		if err != nil {
			return d, err
		}
		aurInstalled, err := r.queryPackageNames("pacman", "-Qqem")
		if err != nil {
			return d, err
		}
		nativeInstalled = filterIgnoredNames(nativeInstalled, ignored)
		aurInstalled = filterIgnoredNames(aurInstalled, ignored)

		d.Native = packageDrift(nativeState, nativeInstalled)
		d.AUR = packageDrift(aurState, aurInstalled)
	}

	if layers.Managed {
		configState, err := readStateFileStrictOptional(repo.configPath(), validateManagedEntry)
		if err != nil {
			return d, err
		}
		homeState, err := readStateFileStrictOptional(repo.homePath(), validateManagedEntry)
		if err != nil {
			return d, err
		}
		d.Config = planConfigs(repo, configState, BootstrapOptions{})
		d.Home = planHomeFiles(repo, homeState, BootstrapOptions{})
	}
	return d, nil
}

func packageDrift(tracked map[string]string, installed []string) PackageDrift {
	return PackageDrift{
		Missing:   missingPackages(tracked, installed),
		Untracked: untrackedPackages(installed, tracked),
	}
}

func untrackedPackages(installed []string, tracked map[string]string) []string {
	untracked := make([]string, 0)
	for _, name := range installed {
		if _, ok := tracked[name]; !ok {
			untracked = append(untracked, name)
		}
	}
	return uniqueSorted(untracked)
}

func (r *Runner) printStatus(native, aur PackageDrift, configActions, homeActions []ManagedAction) {
	fmt.Fprintln(r.Stdout, "Package status:")
	printPackageList(r.Stdout, "native missing", native.Missing)
	printPackageList(r.Stdout, "native untracked", native.Untracked)
	printPackageList(r.Stdout, "AUR missing", aur.Missing)
	printPackageList(r.Stdout, "AUR untracked", aur.Untracked)

	printManagedStatus(r.Stdout, "Config status:", "no config entries declared", configActions)
	printManagedStatus(r.Stdout, "Home file status:", "no home files declared", homeActions)
}

func printManagedStatus(w io.Writer, title, empty string, actions []ManagedAction) {
	printManagedSection(w, title, empty, actions, func(w io.Writer, action ManagedAction) {
		switch action.Kind {
		case ManagedNoopAction:
			fmt.Fprintf(w, "  ok %s\n", action.Name)
		case ManagedSymlinkAction:
			fmt.Fprintf(w, "  missing %s: needs link %s -> %s\n", action.Name, action.LocalPath, action.RepoPath)
		case ManagedConflictAction:
			fmt.Fprintf(w, "  conflict %s: %s\n", action.Name, action.Message)
		case ManagedErrorAction:
			fmt.Fprintf(w, "  error %s: %v\n", action.Name, action.Err)
		case ManagedAdoptAction:
			fmt.Fprintf(w, "  adopt %s: %s -> %s%s\n", action.Name, action.LocalPath, action.RepoPath, replacingSuffix(action))
		case ManagedRestoreAction:
			fmt.Fprintf(w, "  restore %s: %s -> %s\n", action.Name, action.RepoPath, action.LocalPath)
		}
	})
}
