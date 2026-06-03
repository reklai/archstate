package archstate

import "fmt"

type PackageDrift struct {
	Missing   []string
	Untracked []string
}

func (r *Runner) runStatus() error {
	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}
	nativeState, err := readStateFileStrict(repo.pacmanPath(), validatePackageEntry)
	if err != nil {
		return err
	}
	aurState, err := readStateFileStrict(repo.aurPath(), validatePackageEntry)
	if err != nil {
		return err
	}
	configState, err := readStateFileStrict(repo.configPath(), validateManagedEntry)
	if err != nil {
		return err
	}
	homeState, err := readStateFileStrictOptional(repo.homePath(), validateManagedEntry)
	if err != nil {
		return err
	}

	nativeInstalled, err := r.queryPackageNames("pacman", "-Qqen")
	if err != nil {
		return err
	}
	aurInstalled, err := r.queryPackageNames("pacman", "-Qqem")
	if err != nil {
		return err
	}

	native := packageDrift(nativeState, nativeInstalled)
	aur := packageDrift(aurState, aurInstalled)
	configActions := planConfigs(repo, configState, BootstrapOptions{})
	homeActions := planHomeFiles(repo, homeState, BootstrapOptions{})

	r.printStatus(native, aur, configActions, homeActions)
	return nil
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

func printManagedStatus(w interface{ Write([]byte) (int, error) }, title, empty string, actions []ManagedAction) {
	fmt.Fprintln(w, title)
	if len(actions) == 0 {
		fmt.Fprintf(w, "  %s\n", empty)
		return
	}
	for _, action := range actions {
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
			fmt.Fprintf(w, "  adopt %s: %s -> %s\n", action.Name, action.LocalPath, action.RepoPath)
		case ManagedOverwriteAction:
			fmt.Fprintf(w, "  overwrite %s: %s -> %s\n", action.Name, action.RepoPath, action.LocalPath)
		}
	}
}
