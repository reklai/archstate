package archstate

import (
	"fmt"
	"io"
	"strings"
)

// VerifyOptions controls which layers of machineDrift the check exit gates
// (check --exit / check --gate) fail on.
type VerifyOptions struct {
	// StrictPackages fails when explicit packages are installed but not tracked.
	StrictPackages bool
	// PackagesOnly skips config/home checks.
	PackagesOnly bool
	// DotFilesOnly skips package checks.
	DotFilesOnly bool
	// Label is the verb used in "label: ok/failed" messaging (default "check").
	Label string
}

func (r *Runner) reportVerify(d machineDrift, opts VerifyOptions) error {
	label := opts.Label
	if label == "" {
		label = "check"
	}
	var failures []string
	var packageMissing, packageUntracked bool
	var managedMissing, managedConflict, managedOther bool

	if !opts.DotFilesOnly {
		if len(d.Native.Missing) > 0 {
			packageMissing = true
			failures = append(failures, fmt.Sprintf("native missing: %s", strings.Join(d.Native.Missing, " ")))
		}
		if len(d.AUR.Missing) > 0 {
			packageMissing = true
			failures = append(failures, fmt.Sprintf("AUR missing: %s", strings.Join(d.AUR.Missing, " ")))
		}
		if opts.StrictPackages {
			if len(d.Native.Untracked) > 0 {
				packageUntracked = true
				failures = append(failures, fmt.Sprintf("native untracked: %s", strings.Join(d.Native.Untracked, " ")))
			}
			if len(d.AUR.Untracked) > 0 {
				packageUntracked = true
				failures = append(failures, fmt.Sprintf("AUR untracked: %s", strings.Join(d.AUR.Untracked, " ")))
			}
		}
	}

	if !opts.PackagesOnly {
		configFails := managedVerifyFailures("config", d.Config)
		homeFails := managedVerifyFailures("home", d.Home)
		failures = append(failures, configFails...)
		failures = append(failures, homeFails...)
		managedMissing, managedConflict, managedOther = classifyManagedVerifyFailures(d.Config, d.Home)
	}

	if len(failures) == 0 {
		fmt.Fprintln(r.Stdout, label+": ok")
		return nil
	}

	fmt.Fprintln(r.Stdout, label+": failed")
	for _, line := range failures {
		fmt.Fprintf(r.Stdout, "  %s\n", line)
	}
	printVerifyRemediation(r.Stdout, packageMissing, packageUntracked, managedMissing, managedConflict, managedOther)
	return fmt.Errorf("%s found drift", label)
}

// printVerifyRemediation emits only commands that can clear the observed
// failure classes. Untracked packages need sync/ignore, not apply; dry-run is
// inspection, not a fix for managed drift.
func printVerifyRemediation(w io.Writer, packageMissing, packageUntracked, managedMissing, managedConflict, managedOther bool) {
	fmt.Fprintln(w, "inspect: archstate check")
	if packageMissing {
		fmt.Fprintln(w, "fix packages: archstate apply --packages")
	}
	if packageUntracked {
		fmt.Fprintln(w, "accept untracked packages: archstate sync")
		fmt.Fprintln(w, "or ignore: archstate ignore add <pkg>")
	}
	if managedMissing {
		fmt.Fprintln(w, "fix missing links: archstate apply --dotfiles")
	}
	if managedConflict {
		fmt.Fprintln(w, "inspect file conflicts: archstate apply --dry-run")
		fmt.Fprintln(w, "fix keep local: archstate apply --adopt")
		fmt.Fprintln(w, "fix keep tracked: archstate apply --restore")
	}
	if managedOther {
		fmt.Fprintln(w, "inspect managed errors: archstate check")
		fmt.Fprintln(w, "or stop tracking: archstate track config|home rm <name>")
	}
}

func classifyManagedVerifyFailures(config, home []ManagedAction) (missing, conflict, other bool) {
	for _, actions := range [][]ManagedAction{config, home} {
		for _, action := range actions {
			switch action.Kind {
			case ManagedNoopAction:
				continue
			case ManagedSymlinkAction:
				missing = true
			case ManagedConflictAction:
				conflict = true
			default:
				other = true
			}
		}
	}
	return missing, conflict, other
}

func managedVerifyFailures(kind string, actions []ManagedAction) []string {
	var failures []string
	for _, action := range actions {
		switch action.Kind {
		case ManagedNoopAction:
			continue
		case ManagedSymlinkAction:
			failures = append(failures, fmt.Sprintf("%s missing: %s needs link %s -> %s", kind, action.Name, action.LocalPath, action.RepoPath))
		case ManagedConflictAction:
			failures = append(failures, fmt.Sprintf("%s conflict: %s: %s", kind, action.Name, action.Message))
		case ManagedErrorAction:
			failures = append(failures, fmt.Sprintf("%s error: %s: %v", kind, action.Name, action.Err))
		default:
			// adopt/restore only appear with bootstrap flags; plain plan should not emit them
			failures = append(failures, fmt.Sprintf("%s unexpected: %s (%s)", kind, action.Name, action.Kind))
		}
	}
	return failures
}
