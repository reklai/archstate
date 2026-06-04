package archstate

import (
	"errors"
	"fmt"
	"io"
)

type doctorReport struct {
	w  io.Writer
	ok bool
}

func newDoctorReport(w io.Writer) *doctorReport {
	return &doctorReport{w: w, ok: true}
}

func (d *doctorReport) OK(label, message string) {
	if message == "" {
		fmt.Fprintf(d.w, "OK %s\n", label)
		return
	}
	fmt.Fprintf(d.w, "OK %s: %s\n", label, message)
}

func (d *doctorReport) WARN(label, message string, hints ...string) {
	fmt.Fprintf(d.w, "WARN %s: %s\n", label, message)
	for _, hint := range hints {
		fmt.Fprintf(d.w, "  %s\n", hint)
	}
}

func (d *doctorReport) ERROR(label string, err error, hints ...string) {
	d.ok = false
	fmt.Fprintf(d.w, "ERROR %s: %v\n", label, err)
	for _, hint := range hints {
		fmt.Fprintf(d.w, "  %s\n", hint)
	}
}

func (d *doctorReport) ERRORMessage(label, message string, hints ...string) {
	d.ERROR(label, errors.New(message), hints...)
}

func (r *Runner) runDoctor() error {
	repo, err := r.discoverRepo()
	if err != nil {
		return err
	}
	report := newDoctorReport(r.Stdout)

	initialized := true
	if err := requireInitialized(repo); err != nil {
		initialized = false
		report.ERROR("repo", err, "fix: archstate init")
	} else {
		report.OK("repo", repo.path)
	}

	if path, err := r.lookPath("pacman"); err != nil {
		report.ERROR("pacman command", err, "fix: install pacman or run on an Arch-based system")
	} else {
		report.OK("pacman command", path)
	}
	if path, err := r.lookPath("sudo"); err != nil {
		report.ERROR("sudo command", err, "fix: install sudo or make sudo available in PATH")
	} else {
		report.OK("sudo command", path)
	}

	if !initialized {
		if !report.ok {
			return errors.New("doctor found problems")
		}
		return nil
	}

	pacmanEntries, pacmanErr := readStateFileStrictOptional(repo.pacmanPath(), validatePackageEntry)
	reportStateFile(report, pacmanConfFile, pacmanErr, "fix: archstate sync", "restore: archstate snapshot restore <id>")
	aurEntries, aurErr := readStateFileStrictOptional(repo.aurPath(), validatePackageEntry)
	reportStateFile(report, aurConfFile, aurErr, "fix: archstate sync", "restore: archstate snapshot restore <id>")
	configEntries, configErr := readStateFileStrictOptional(repo.configPath(), validateManagedEntry)
	reportStateFile(report, configConfFile, configErr, "inspect: archstate help config", "restore: archstate snapshot restore <id>")
	homeEntries, homeErr := readStateFileStrictOptional(repo.homePath(), validateManagedEntry)
	reportStateFile(report, homeConfFile, homeErr, "inspect: archstate help home", "restore: archstate snapshot restore <id>")

	if aurErr == nil && len(aurEntries) > 0 {
		if helper, helperPath, _, err := r.resolveAURHelper(""); err != nil {
			report.ERROR("AUR helper", err,
				"fix: archstate bootstrap --aur-helper paru",
				"fix: archstate bootstrap --aur-helper yay",
			)
		} else {
			report.OK("AUR helper", helper+" at "+helperPath)
		}
	}

	if _, err := r.queryPackageNames("pacman", "-Qq"); err != nil {
		report.ERROR("package access", err, "fix: check pacman access", "inspect: pacman -Qq")
	} else {
		report.OK("package access", "pacman -Qq")
	}

	if pacmanErr == nil && aurErr == nil {
		r.reportPackageDrift(report, pacmanEntries, aurEntries)
	}
	if configErr == nil {
		reportManagedDoctor(report, configRoot(repo), configEntries)
	}
	if homeErr == nil {
		reportManagedDoctor(report, homeRoot(repo), homeEntries)
	}

	if !report.ok {
		return errors.New("doctor found problems")
	}
	return nil
}

func reportStateFile(report *doctorReport, label string, err error, hints ...string) {
	if err != nil {
		report.ERROR(label, err, hints...)
		return
	}
	report.OK(label, "parseable")
}

func (r *Runner) reportPackageDrift(report *doctorReport, nativeState, aurState map[string]string) {
	nativeInstalled, err := r.queryPackageNames("pacman", "-Qqen")
	if err != nil {
		report.WARN("package drift", "could not query explicit native packages", "inspect: archstate status")
		return
	}
	aurInstalled, err := r.queryPackageNames("pacman", "-Qqem")
	if err != nil {
		report.WARN("package drift", "could not query explicit AUR packages", "inspect: archstate status")
		return
	}

	native := packageDrift(nativeState, nativeInstalled)
	aur := packageDrift(aurState, aurInstalled)
	missing := len(native.Missing) + len(aur.Missing)
	untracked := len(native.Untracked) + len(aur.Untracked)
	if missing == 0 && untracked == 0 {
		report.OK("package drift", "tracked packages match explicit packages")
		return
	}
	if missing > 0 {
		report.WARN("package drift", fmt.Sprintf("%d tracked packages are missing", missing),
			"inspect: archstate status",
			"dry-run: archstate bootstrap --dry-run",
			"fix: archstate bootstrap",
		)
	}
	if untracked > 0 {
		report.WARN("package drift", fmt.Sprintf("%d explicit packages are not tracked", untracked),
			"inspect: archstate status",
			"accept current machine: archstate sync",
		)
	}
}

func reportManagedDoctor(report *doctorReport, root managedRoot, entries map[string]string) {
	if len(entries) == 0 {
		report.OK(root.Kind+" health", "no entries declared")
		return
	}
	for _, name := range sortedEntryKeys(entries) {
		action := planManagedEntry(root, name, entries[name], BootstrapOptions{})
		label := root.Kind + " " + name
		switch action.Kind {
		case ManagedNoopAction:
			report.OK(label, "managed symlink is healthy")
		case ManagedSymlinkAction:
			report.ERRORMessage(label, "managed symlink is missing",
				"local: "+action.LocalPath,
				"tracked: "+action.RepoPath,
				"dry-run: archstate bootstrap --dry-run",
				"fix: archstate bootstrap",
			)
		case ManagedConflictAction:
			hints := []string{
				"local: " + action.LocalPath,
				"tracked: " + action.RepoPath,
				"dry-run: archstate bootstrap --dry-run",
				"fix keep local: archstate bootstrap --adopt",
			}
			if pathExists(action.RepoPath) {
				hints = append(hints, "fix restore tracked: archstate bootstrap --overwrite")
			}
			report.ERRORMessage(label, "unmanaged local entry exists", hints...)
		case ManagedErrorAction:
			report.ERROR(label, action.Err,
				"local: "+action.LocalPath,
				"tracked: "+action.RepoPath,
				"stop managing: archstate "+managedCommand(root)+" rm "+name,
				"restore: archstate snapshot restore <id>",
			)
		default:
			report.ERRORMessage(label, fmt.Sprintf("unexpected managed action %q", action.Kind))
		}
	}
}

func managedCommand(root managedRoot) string {
	if root.RepoRoot == homeDirName {
		return "home"
	}
	return "config"
}
