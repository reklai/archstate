package archstate

import (
	"errors"
	"fmt"
)

// CheckOptions controls the unified check command: the default drift/health
// report, single-section subset views, and scriptable exit gates.
type CheckOptions struct {
	// Exit makes check non-zero when packages are missing or managed entries
	// are unhealthy, after printing the full report.
	Exit bool
	// Gate is the compact scriptable gate: only "check: ok/failed" messaging
	// with remediation, no report sections.
	Gate bool
	// StrictPackages also fails on untracked explicit packages when Exit or
	// Gate is set.
	StrictPackages bool
	// PackagesOnly skips config/home checks when Exit or Gate is set.
	PackagesOnly bool
	// DotFilesOnly skips package checks when Exit or Gate is set.
	DotFilesOnly bool
	// Doctor includes doctor-style environment and health reports.
	// Default check enables this.
	Doctor bool
	// StatusOnly prints only package/managed drift (--status).
	StatusOnly bool
	// DoctorOnly prints only doctor reports and fails on ERROR (--doctor).
	DoctorOnly bool
	// CoverageOnly prints only the coverage report (--coverage).
	CoverageOnly bool
}

func parseCheckArgs(args []string) (CheckOptions, error) {
	var opts CheckOptions
	// Default check always includes status + doctor-style health.
	opts.Doctor = true
	for _, arg := range args {
		switch arg {
		case "--exit":
			opts.Exit = true
		case "--gate":
			opts.Gate = true
		case "--strict-packages":
			opts.StrictPackages = true
		case "--packages-only":
			opts.PackagesOnly = true
		case "--dotfiles-only":
			opts.DotFilesOnly = true
		case "--status":
			opts.StatusOnly = true
		case "--doctor":
			opts.DoctorOnly = true
		case "--coverage":
			opts.CoverageOnly = true
		default:
			return CheckOptions{}, fmt.Errorf("usage: archstate check [--status|--doctor|--coverage] [--exit|--gate] [--strict-packages] [--packages-only|--dotfiles-only]")
		}
	}
	if opts.PackagesOnly && opts.DotFilesOnly {
		return CheckOptions{}, errors.New("--packages-only and --dotfiles-only are mutually exclusive")
	}
	if opts.Exit && opts.Gate {
		return CheckOptions{}, errors.New("--exit and --gate are mutually exclusive: --exit prints the full report, --gate stays compact")
	}
	views := 0
	for _, v := range []bool{opts.StatusOnly, opts.DoctorOnly, opts.CoverageOnly} {
		if v {
			views++
		}
	}
	if views > 1 {
		return CheckOptions{}, errors.New("--status, --doctor, and --coverage are mutually exclusive subset views")
	}
	if views == 1 && (opts.Exit || opts.Gate) {
		return CheckOptions{}, errors.New("subset views (--status, --doctor, --coverage) cannot be combined with --exit or --gate")
	}
	if (opts.StrictPackages || opts.PackagesOnly || opts.DotFilesOnly) && !opts.Exit && !opts.Gate {
		return CheckOptions{}, errors.New("--strict-packages, --packages-only, and --dotfiles-only only apply with --exit or --gate")
	}
	return opts, nil
}

func (r *Runner) runCheck(args []string) error {
	opts, err := parseCheckArgs(args)
	if err != nil {
		return err
	}
	return r.executeCheck(opts)
}

// executeCheck is the shared implementation for check and its subset views.
func (r *Runner) executeCheck(opts CheckOptions) error {
	if opts.Gate {
		return r.runGate(VerifyOptions{
			StrictPackages: opts.StrictPackages,
			PackagesOnly:   opts.PackagesOnly,
			DotFilesOnly:   opts.DotFilesOnly,
			Label:          "check",
		})
	}
	if opts.DoctorOnly {
		return r.runDoctor()
	}
	if opts.CoverageOnly {
		return r.runCoverage(nil)
	}
	if opts.StatusOnly {
		repo, err := r.discoverExistingRepo()
		if err != nil {
			return err
		}
		d, err := r.computeMachineDrift(repo)
		if err != nil {
			return err
		}
		r.printStatus(d.Native, d.AUR, d.Config, d.Home)
		return nil
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
	// Use --exit or --gate for a completeness gate.
	if opts.Doctor {
		fmt.Fprintln(r.Stdout)
		_ = r.runDoctor()
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

// runGate is the compact exit gate behind check --gate: it loads only the
// in-scope drift layers and prints check: ok/failed with remediation.
func (r *Runner) runGate(opts VerifyOptions) error {
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
		opts.Label = "check"
	}
	return r.reportVerify(d, opts)
}
