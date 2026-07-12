package archstate

import (
	"errors"
	"fmt"
)

// CheckOptions controls the unified check command (status + optional doctor,
// coverage, and scriptable exit codes).
type CheckOptions struct {
	// Exit makes check non-zero when packages are missing or managed entries
	// are unhealthy (verify semantics).
	Exit bool
	// StrictPackages also fails on untracked explicit packages when Exit is set
	// (or alone for verify-compat via the verify alias).
	StrictPackages bool
	// PackagesOnly skips config/home checks when Exit is set.
	PackagesOnly bool
	// DotFilesOnly skips package checks when Exit is set.
	DotFilesOnly bool
	// Coverage also prints the coverage report.
	Coverage bool
	// Doctor includes doctor-style environment and health reports.
	// Default check enables this; the doctor alias forces it alone.
	Doctor bool
	// StatusOnly prints only package/managed drift (status alias).
	StatusOnly bool
	// DoctorOnly prints only doctor reports (doctor alias).
	DoctorOnly bool
	// CoverageOnly prints only coverage (coverage alias).
	CoverageOnly bool
	// VerifyOnly uses verify-style messaging without status/doctor listing
	// (verify alias).
	VerifyOnly bool
}

func parseCheckArgs(args []string) (CheckOptions, error) {
	var opts CheckOptions
	// Primary check always includes status + doctor-style health.
	opts.Doctor = true
	for _, arg := range args {
		switch arg {
		case "--exit":
			opts.Exit = true
		case "--strict-packages":
			opts.StrictPackages = true
		case "--packages-only":
			opts.PackagesOnly = true
		case "--dotfiles-only":
			opts.DotFilesOnly = true
		case "--coverage":
			opts.Coverage = true
		default:
			return CheckOptions{}, fmt.Errorf("usage: archstate check [--coverage] [--exit] [--strict-packages] [--packages-only|--dotfiles-only]")
		}
	}
	if opts.PackagesOnly && opts.DotFilesOnly {
		return CheckOptions{}, errors.New("--packages-only and --dotfiles-only are mutually exclusive")
	}
	// --strict-packages without --exit is allowed (verify-compat and scripts
	// that want the flag recorded); it only affects the exit decision.
	return opts, nil
}

func (r *Runner) runCheck(args []string) error {
	opts, err := parseCheckArgs(args)
	if err != nil {
		return err
	}
	return r.executeCheck(opts)
}

// executeCheck is the shared implementation for check and its aliases.
func (r *Runner) executeCheck(opts CheckOptions) error {
	if opts.VerifyOnly {
		return r.runVerifyWith(VerifyOptions{
			StrictPackages: opts.StrictPackages,
			PackagesOnly:   opts.PackagesOnly,
			DotFilesOnly:   opts.DotFilesOnly,
			Label:          "verify",
		})
	}
	if opts.DoctorOnly {
		return r.runDoctor()
	}
	if opts.CoverageOnly {
		return r.runCoverage(nil)
	}
	if opts.StatusOnly {
		return r.runStatus()
	}

	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}

	// Collect status when possible. Failures (missing pacman, broken state
	// files) must not skip the doctor section — those are exactly the cases
	// where the primary diagnostic should print environment/health.
	d, driftErr := r.computeMachineDrift(repo)
	if driftErr == nil {
		r.printStatus(d.Native, d.AUR, d.Config, d.Home)
	} else {
		fmt.Fprintf(r.Stdout, "Package status: unavailable (%v)\n", driftErr)
	}

	// Doctor-style health (env + state + compact drift/managed) where practical.
	// Printed after status; informational only — ERROR lines do not fail the run.
	// Use --exit for a completeness gate (verify semantics, not doctor aggregation).
	if opts.Doctor {
		fmt.Fprintln(r.Stdout)
		_ = r.runDoctor()
	}

	if opts.Coverage {
		fmt.Fprintln(r.Stdout)
		if err := r.runCoverage(nil); err != nil {
			// Prefer returning coverage errors only when drift succeeded so a
			// broken package layer does not hide an independent coverage failure;
			// when drift already failed, keep that as the primary error below.
			if driftErr == nil {
				return err
			}
		}
	}

	if opts.Exit {
		if driftErr != nil {
			return driftErr
		}
		return r.reportVerify(d, VerifyOptions{
			StrictPackages: opts.StrictPackages,
			PackagesOnly:   opts.PackagesOnly,
			DotFilesOnly:   opts.DotFilesOnly,
			Label:          "check",
		})
	}
	if driftErr != nil {
		return driftErr
	}
	return nil
}

func (r *Runner) runVerifyWith(opts VerifyOptions) error {
	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}
	// Load only in-scope layers so --dotfiles-only never requires pacman and
	// --packages-only never fails on a broken config/home file.
	d, err := r.computeMachineDriftLayers(repo, driftLayersForVerify(opts))
	if err != nil {
		return err
	}
	if opts.Label == "" {
		opts.Label = "verify"
	}
	return r.reportVerify(d, opts)
}
