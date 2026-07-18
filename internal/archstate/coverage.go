package archstate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type coverageBucket struct {
	Tracked  int
	Addable  int
	Symlink  int
	Deny     int
	AddNames []string
}

func (r *Runner) runCoverage(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: archstate check --coverage")
	}
	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}

	configRootLocal := filepath.Join(repo.home, ".config")
	configExclude := map[string]bool{}
	if filepath.Dir(repo.path) == configRootLocal {
		configExclude[filepath.Base(repo.path)] = true
	}
	configEntries, err := readStateFileStrictOptional(repo.configPath(), validateManagedEntry)
	if err != nil {
		return err
	}
	configCov, err := scanManagedCoverage(repo, configRoot(repo), configEntries, configRootLocal, false, configExclude)
	if err != nil {
		return err
	}

	homeExclude := map[string]bool{".config": true, ".cache": true, ".local": true}
	homeEntries, err := readStateFileStrictOptional(repo.homePath(), validateManagedEntry)
	if err != nil {
		return err
	}
	homeCov, err := scanManagedCoverage(repo, homeRoot(repo), homeEntries, repo.home, true, homeExclude)
	if err != nil {
		return err
	}

	printCoverageSection(r.Stdout, "Config coverage (~/.config direct children):", configCov)
	printCoverageSection(r.Stdout, "Home coverage (~ dotfiles, excluding .config/.cache/.local):", homeCov)

	totalTracked := configCov.Tracked + homeCov.Tracked
	totalAddable := configCov.Addable + homeCov.Addable
	totalConsidered := totalTracked + totalAddable + configCov.Symlink + homeCov.Symlink + configCov.Deny + homeCov.Deny
	if totalConsidered == 0 {
		fmt.Fprintln(r.Stdout, "Overall: nothing to track under scanned roots")
		return nil
	}
	// Completeness metric: tracked / (tracked + addable). Symlink/deny are blocked, not debt.
	denom := totalTracked + totalAddable
	pct := 100
	if denom > 0 {
		pct = (totalTracked * 100) / denom
	}
	fmt.Fprintf(r.Stdout, "Overall: %d/%d addable entries tracked (%d%%); %d blocked (symlink/deny)\n",
		totalTracked, denom, pct, configCov.Symlink+homeCov.Symlink+configCov.Deny+homeCov.Deny)
	return nil
}

func scanManagedCoverage(repo repoPaths, root managedRoot, entries map[string]string, localRoot string, dotfilesOnly bool, exclude map[string]bool) (coverageBucket, error) {
	var cov coverageBucket
	dirEntries, err := os.ReadDir(localRoot)
	if err != nil && !os.IsNotExist(err) {
		return cov, err
	}
	for _, entry := range dirEntries {
		name := entry.Name()
		if exclude[name] || (dotfilesOnly && !strings.HasPrefix(name, ".")) {
			continue
		}
		if _, tracked := entries[name]; tracked {
			cov.Tracked++
			continue
		}
		info, err := os.Lstat(root.LocalPath(name))
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			cov.Symlink++
			continue
		}
		if isSensitiveName(repo, name) {
			cov.Deny++
			continue
		}
		cov.Addable++
		cov.AddNames = append(cov.AddNames, name)
	}
	// Count tracked entries that are only in state (local missing still "tracked" intent).
	for name := range entries {
		// already counted if present under localRoot; if not present, still tracked
		path := root.LocalPath(name)
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			cov.Tracked++
		}
	}
	return cov, nil
}

func printCoverageSection(w io.Writer, title string, cov coverageBucket) {
	fmt.Fprintln(w, title)
	fmt.Fprintf(w, "  tracked: %d\n", cov.Tracked)
	fmt.Fprintf(w, "  addable: %d\n", cov.Addable)
	fmt.Fprintf(w, "  symlink: %d\n", cov.Symlink)
	fmt.Fprintf(w, "  deny:    %d\n", cov.Deny)
	if len(cov.AddNames) > 0 {
		fmt.Fprintf(w, "  add next: %s\n", strings.Join(cov.AddNames, " "))
	}
}
