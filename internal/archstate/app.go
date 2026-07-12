package archstate

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Runner struct {
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
	Cwd            string
	Home           string
	Env            []string
	Now            func() time.Time
	ExecutablePath string

	packageRemovalTUI packageRemovalTUIFunc
	managedRemovalTUI managedRemovalTUIFunc
}

func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	r := &Runner{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	}
	if err := r.Run(args); err != nil {
		fmt.Fprintf(stderr, "archstate: %v\n", err)
		return 1
	}
	return 0
}

func (r *Runner) Run(args []string) error {
	r.setDefaults()
	if len(args) == 0 {
		r.printHelp()
		return nil
	}

	switch args[0] {
	case "help", "-h", "--help":
		if args[0] != "help" && len(args) != 1 {
			return fmt.Errorf("usage: archstate help [command]")
		}
		if args[0] == "help" && len(args) > 2 {
			return fmt.Errorf("usage: archstate help [command]")
		}
		if len(args) == 2 && isHelpArg(args[1]) {
			r.printHelp()
			return nil
		}
		if len(args) == 2 {
			return r.printCommandHelp(args[1])
		}
		r.printHelp()
		return nil
	case "init":
		if len(args) == 2 && isHelpArg(args[1]) {
			return r.printCommandHelp("init")
		}
		install, err := parseInitArgs(args[1:])
		if err != nil {
			return err
		}
		return r.runInit(install)
	case "install":
		if len(args) == 2 && isHelpArg(args[1]) {
			return r.printCommandHelp("install")
		}
		addToPath, err := parseInstallArgs(args[1:])
		if err != nil {
			return err
		}
		return r.runInstall(addToPath)
	case "sync":
		if len(args) == 2 && isHelpArg(args[1]) {
			return r.printCommandHelp("sync")
		}
		commit, err := parseSyncArgs(args[1:])
		if err != nil {
			return err
		}
		return r.runSync(commit)
	case "packages":
		if len(args) >= 2 && args[1] == "ignore" {
			return r.runPackagesIgnore(args[2:])
		}
		return r.runPackages(args[1:])
	case "check":
		if len(args) == 2 && isHelpArg(args[1]) {
			return r.printCommandHelp("check")
		}
		return r.runCheck(args[1:])
	case "status":
		// Alias of check without --exit (drift listing only).
		if len(args) == 2 && isHelpArg(args[1]) {
			return r.printCommandHelp("status")
		}
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate status")
		}
		return r.executeCheck(CheckOptions{StatusOnly: true})
	case "verify":
		// Alias of check --exit.
		if len(args) == 2 && isHelpArg(args[1]) {
			return r.printCommandHelp("verify")
		}
		return r.runVerify(args[1:])
	case "coverage":
		// Alias of check --coverage.
		if len(args) == 2 && isHelpArg(args[1]) {
			return r.printCommandHelp("coverage")
		}
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate coverage")
		}
		return r.executeCheck(CheckOptions{CoverageOnly: true})
	case "doctor":
		// Alias of check with doctor-style reports.
		if len(args) == 2 && isHelpArg(args[1]) {
			return r.printCommandHelp("doctor")
		}
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate doctor")
		}
		return r.executeCheck(CheckOptions{DoctorOnly: true})
	case "track":
		return r.runTrack(args[1:])
	case "managed":
		// Alias of bare track (managed untrack TUI).
		return r.runManaged(args[1:])
	case "config":
		// Alias of track config.
		return r.runConfig(args[1:])
	case "home":
		// Alias of track home.
		return r.runHome(args[1:])
	case "apply":
		// Primary name for bootstrap.
		return r.runApply(args[1:])
	case "bootstrap":
		// Permanent alias of apply.
		return r.runBootstrap(args[1:])
	case "snapshot":
		return r.runSnapshot(args[1:])
	case "service":
		return r.runService(args[1:])
	default:
		return fmt.Errorf("unknown command %q; run 'archstate help' to see available commands", args[0])
	}
}

func (r *Runner) setDefaults() {
	if r.Stdin == nil {
		r.Stdin = os.Stdin
	}
	if r.Stdout == nil {
		r.Stdout = os.Stdout
	}
	if r.Stderr == nil {
		r.Stderr = os.Stderr
	}
	if r.Cwd == "" {
		if cwd, err := os.Getwd(); err == nil {
			r.Cwd = cwd
		}
	}
	if r.Home == "" {
		if home, err := os.UserHomeDir(); err == nil {
			r.Home = home
		}
	}
	if r.Env == nil {
		r.Env = os.Environ()
	}
}

func (r *Runner) printHelp() {
	fmt.Fprintln(r.Stdout, `Archstate tracks explicit Arch packages, ~/.config entries, and selected home files.

Usage:
  archstate <command> [options]

Repo:
  ~/.config/archstate-src

Common workflow:
  archstate init
  archstate sync
  archstate track config add nvim
  archstate track home add .zshrc
  archstate check
  archstate apply --dry-run
  archstate apply

Commands:
  init       Create repo state and install archstate to ~/.local/bin.
  sync       Capture explicit packages from this machine.
  track      Add/list/preview/rm config & home (TUI untrack with no args).
  check      Show drift/health; --exit / --strict-packages for scripts; --coverage.
  apply      Install missing packages and recreate managed symlinks.
  snapshot   Save, list, restore, or remove repo-state snapshots.

Also:
  packages   Fuzzy-select explicit packages to remove; manage package ignores.
  service    Manage the optional systemd user sync timer.
  install    Install or update archstate in ~/.local/bin.

Aliases (still work; legacy entry points, not always identical output):
  status     drift listing only (subset of check)
  verify     exit-code gate (same checks as check --exit; compact messaging)
  doctor     health report only (fails on ERROR)
  coverage   coverage report only
  config, home, managed  -> track
  bootstrap              -> apply

Command help:
  archstate help <command>
  archstate <command> --help

Examples:
  archstate track config add nvim
  archstate snapshot list --manual
  archstate apply --dry-run`)
}

func isHelpArg(arg string) bool {
	return arg == "--help" || arg == "-h"
}

func (r *Runner) printCommandHelp(topic string) error {
	switch topic {
	case "init":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate init [--no-install]

Create ~/.config/archstate-src, state files, config/home directories, and install
this archstate binary to ~/.local/bin/archstate.

Options:
  --no-install  Create repo state without installing the binary.

Examples:
  archstate init
  archstate init --no-install`)
	case "install":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate install [--add-to-path]

Install or update this archstate binary at ~/.local/bin/archstate.

Prefer archstate init for first-time setup (creates the repo and installs the
CLI). Use install when you only need to update the binary.

Options:
  --add-to-path  Append the PATH line to your shell rc file (bash/zsh/fish/sh).

If ~/.local/bin is not in PATH, archstate prints the exact rc file and line to
add. Without --add-to-path it never edits shell files.`)
	case "sync":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate sync [--commit]

Rewrite package state from this machine's explicit packages.

Sources:
  pacman -Qqen  -> pacman.conf
  pacman -Qqem  -> aur.conf

Options:
  --commit  In a git repo, commit pacman.conf and aur.conf after a rewrite.
            The systemd timer uses this so background syncs do not leave the
            repo dirty.

Notes:
  Existing package descriptions are preserved by package name.
  Malformed package-file lines, comments, and blanks are cleaned up.
  If package files already match this machine, sync does not snapshot or rewrite.
  An automatic snapshot is created before package files are rewritten.`)
	case "packages":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate packages
  archstate packages ignore add <pkg>...
  archstate packages ignore rm <pkg>...
  archstate packages ignore list

Open an interactive package removal TUI, or manage the package ignore list.

packages (no args):
  sync package state before opening the TUI
  fuzzy-search Native or AUR packages
  mark packages for removal
  review the marked packages, then run one sudo pacman -Rns command
  sync package state again after successful removal

packages ignore:
  add <pkg>  Do not track these explicit packages in pacman.conf/aur.conf.
  rm <pkg>   Stop ignoring packages (they reappear on the next sync if installed).
  list       Show ignored package names.

Ignored packages are skipped by sync and are not reported as untracked by status,
doctor, or verify --strict-packages. They are never installed by apply/bootstrap.

Keys (removal TUI):
  1/2       switch Native/AUR section
  type      fuzzy-search the active section
  up/down   move the cursor
  tab       switch between the package list and the marked list
  f or /    focus the search field
  space     mark or unmark the highlighted package
  enter     review marked packages, then confirm removal
  q         quit
  esc       quit, or go back from the review page

Review page:
  up/down   move through the packages to remove
  space     unmark a package
  enter/y   run the removal
  esc/n     go back to the package list`)
	case "check":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate check [--coverage] [--exit] [--strict-packages] [--packages-only|--dotfiles-only]

Show package/managed drift and doctor-style health without changing anything.

Default reports:
  tracked native/AUR packages that are missing or untracked
  managed config and home entries as ok, missing, conflict, or error
  environment and repo health (OK/WARN/ERROR)

Notes:
  Default check is informational: ERROR/WARN lines may appear and exit is still 0.
  Only --exit (or the verify alias) is a completeness gate for scripts.

Options:
  --coverage         Also print config/home coverage report after drift/health.
  --exit             Non-zero if missing packages or unhealthy managed entries.
  --strict-packages  With --exit, also fail on untracked explicit packages.
  --packages-only    With --exit, check packages only; skip config/home.
  --dotfiles-only    With --exit, check config/home only; skip packages.

Aliases (legacy subsets; not identical to flag combinations above):
  status     drift listing only
  verify     exit-code gate with compact verify: ok/failed messaging
  doctor     doctor-style health only (fails on ERROR)
  coverage   coverage report only

Examples:
  archstate check
  archstate check --exit
  archstate check --exit --strict-packages
  archstate check --coverage`)
	case "status":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate status

Legacy alias: drift listing only (a subset of archstate check; no doctor section).

Reports:
  tracked native/AUR packages that are missing
  explicitly installed native/AUR packages that are not tracked
  managed config and home entries as ok, missing, conflict, or error

Prefer: archstate check`)
	case "verify":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate verify [--strict-packages] [--packages-only|--dotfiles-only]

Legacy exit-code gate (same checks as check --exit; compact verify messaging only,
without the status/doctor listing that primary check --exit prints first).

Checks (by default):
  no tracked packages are missing
  every managed config/home entry is a healthy symlink

Options:
  --strict-packages  Also fail when explicit packages are installed but not tracked.
  --packages-only    Check packages only; skip config/home.
  --dotfiles-only    Check config/home only; skip packages.

Notes:
  verify never mutates state. Use check for a full drift listing and health report.
  Recommended after apply and before relying on a clone.

Prefer: archstate check --exit

Examples:
  archstate verify
  archstate verify --strict-packages
  archstate verify --packages-only`)
	case "coverage":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate coverage

Legacy alias: coverage report only (a subset of archstate check --coverage, which
also prints drift and doctor-style health first).

Report how completely config/home capture covers this machine.

Scans:
  ~/.config direct children (excluding the archstate repo itself)
  ~ dotfiles (excluding .config, .cache, .local)

Counts each entry as tracked, addable, symlink (blocked), or deny (sensitive).
Overall percentage is tracked / (tracked + addable). Does not mutate state.

Use track config preview / track home preview for the full per-name listing.

Prefer: archstate check --coverage`)
	case "track":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate track
  archstate track untrack
  archstate track config add|list|preview|rm ...
  archstate track home add|list|preview|rm ...

Manage config/home capture. Bare track (or track untrack) opens the interactive
untrack TUI (same as managed).

Subcommands:
  config ...  Manage direct children of ~/.config (add/list/preview/rm).
  home ...    Manage direct children of ~ (add/list/preview/rm).
  (no args)   Fuzzy-select managed entries to stop tracking.

Aliases:
  config   track config
  home     track home
  managed  bare track

Examples:
  archstate track config add nvim kitty
  archstate track home add .zshrc
  archstate track config preview
  archstate track`)
	case "managed":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate managed

Alias of archstate track (no args). Open an interactive TUI to stop managing
tracked config and home entries.

This is not package uninstall and does not delete your files. For each selected
entry Archstate removes tracking, restores the local copy when it is a managed
symlink, and deletes the tracked copy from the repo (same as config/home rm).

Behavior:
  list tracked Config and Home entries with health status
  fuzzy-search and mark entries to untrack
  review the marked list, then apply in one batch (one auto-snapshot)

Keys:
  1/2       switch Config/Home section
  type      fuzzy-search the active section
  up/down   move the cursor
  tab       switch between the entry list and the marked list
  f or /    focus the search field
  space     mark or unmark the highlighted entry
  enter     review marked entries, then confirm
  q         quit
  esc       quit, or go back from the review page

Non-interactive alternative:
  archstate track config rm <name>...
  archstate track home rm <name>...

Prefer: archstate track`)
	case "config":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate config add [--force-sensitive] <name>...
  archstate config list
  archstate config preview
  archstate config rm <name>...

Alias of archstate track config. Manage direct children of ~/.config.
add and rm accept multiple names.

Commands:
  add <name>  Save ~/.config/<name> into Archstate config/ and replace it with a symlink.
  list        Show currently tracked config entries.
  preview     Show ~/.config entries and which ones can be added.
  rm <name>   Stop managing ~/.config/<name>, restore it locally, and remove the saved copy.

Options:
  --force-sensitive  Allow tracking sensitive names denied by default (.ssh, gcloud, …).

Prefer: archstate track config ...

Examples:
  archstate config add nvim kitty ghostty
  archstate config preview
  archstate config rm nvim`)
	case "home":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate home add [--force-sensitive] <name>...
  archstate home list
  archstate home preview
  archstate home rm <name>...

Alias of archstate track home. Manage direct children of ~, such as shell/session
files. add and rm accept multiple names.

Commands:
  add <name>  Save ~/<name> into Archstate home/ and replace it with a symlink.
  list        Show currently tracked home entries.
  preview     Show ~ dotfiles and which ones can be added.
  rm <name>   Stop managing ~/<name>, restore it locally, and remove the saved copy.

Options:
  --force-sensitive  Allow tracking sensitive names denied by default (.ssh, .gnupg, …).

Prefer: archstate track home ...

Examples:
  archstate home add .zshrc .profile
  archstate home preview
  archstate home rm .zshrc`)
	case "snapshot":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate snapshot save <name>
  archstate snapshot list [--manual|--auto]
  archstate snapshot restore <id>
  archstate snapshot rm <id>

Snapshots capture Archstate repo state only: package files, config/home files,
and config/home directories. They do not capture installed packages or live home files.

Commands:
  save <name>   Save a named manual snapshot.
  list          List snapshots. Use --manual or --auto to filter.
  restore <id>  Restore Archstate repo state from a snapshot.
  rm <id>       Remove a snapshot.

Notes:
  Manual snapshots are kept until removed.
  Automatic snapshots are silently pruned to the latest 5.
  Restore creates an automatic snapshot first, so the current repo state can be recovered.`)
	case "apply", "bootstrap":
		verb := "apply"
		if topic == "bootstrap" {
			verb = "bootstrap"
		}
		fmt.Fprintf(r.Stdout, `Usage:
  archstate %s --dry-run
  archstate %s [--adopt|--restore] [--aur-helper paru|yay]
  archstate %s --dotfiles [--adopt|--restore]
  archstate %s --packages [--aur-helper paru|yay]

Install missing packages and create managed config/home symlinks.

Each managed entry has two copies: the local one (~/.config/<name> or ~/<name>)
and the tracked one saved in the repo. Apply links local to tracked. When a
real local file already exists and differs, that is a conflict you resolve with
--adopt (keep local) or --restore (keep tracked).

Options:
  --dry-run              Show planned installs, symlinks, conflicts, adoptions, or restores.
  --dotfiles             Apply only config/home symlinks; skip packages (needs no sudo or pacman).
  --packages             Install only packages; skip config/home symlinks (ignores file conflicts).
  --aur-helper paru|yay  Use the selected AUR helper. If missing, bootstrap the matching helper.
  --adopt                Keep the local entry: save it into Archstate, then symlink.
                         If a tracked copy already exists it is replaced (shown as
                         "replacing tracked copy" in --dry-run; an auto-snapshot is taken first).
  --restore              Keep the tracked entry: install the Archstate copy over the local one.
                         Fails if no tracked copy exists yet (use --adopt instead).
  --force-sensitive      With --adopt, allow sensitive names denied by default.

Conflict behavior:
  A plain apply stops on the first unmanaged config/home conflict and installs
  nothing, so package installs and file decisions never get mixed silently. Resolve
  per entry with 'archstate track config/home add/rm', or resolve them all at once
  with --adopt or --restore. Use --packages to install packages now and deal with
  file conflicts later. Risky actions (adopt/restore) auto-snapshot first.

`, verb, verb, verb, verb)
		if topic == "bootstrap" {
			fmt.Fprintln(r.Stdout, `Note: bootstrap is a permanent alias of apply. Prefer archstate apply.

Examples:
  archstate bootstrap --dry-run
  archstate bootstrap --dotfiles --restore
  archstate bootstrap --packages
  archstate bootstrap --aur-helper paru
  archstate bootstrap --adopt
  archstate bootstrap --restore`)
		} else {
			fmt.Fprintln(r.Stdout, `Alias: bootstrap (same flags and behavior).

Examples:
  archstate apply --dry-run
  archstate apply --dotfiles --restore
  archstate apply --packages
  archstate apply --aur-helper paru
  archstate apply --adopt
  archstate apply --restore`)
		}
	case "doctor":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate doctor

Legacy alias: doctor-style health report only (fails on ERROR). Primary check also
includes this section but stays exit 0 unless --exit is set.

Validate repo discovery, required commands, config parseability, package access,
AUR helper availability, package drift, and managed symlink health.

Output convention:
  OK     Healthy checks.
  WARN   Drift or incomplete information that does not block the repo.
  ERROR  Problems that need a fix before Archstate can be trusted.

Doctor prints exact next commands when a fix is known.

Prefer: archstate check`)
	case "service":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate service install
  archstate service enable
  archstate service status
  archstate service disable
  archstate service uninstall

Manage the optional systemd user timer that runs archstate sync --commit.

Commands:
  install    Install/update ~/.local/bin/archstate and write user unit files.
  enable     Enable and start the timer.
  status     Show unit file, enabled, and active state.
  disable    Disable and stop the timer.
  uninstall  Disable the timer and remove Archstate user unit files.

Timer:
  OnBootSec=5min
  OnUnitActiveSec=1h
  RandomizedDelaySec=10min

Notes:
  The service is opt-in; init does not enable it.
  sync no-ops when package state is already current.
  In a git repo, --commit commits pacman.conf and aur.conf so background syncs
  do not leave package-state changes uncommitted (needs user.name/user.email
  configured).`)
	default:
		return fmt.Errorf("unknown help topic %q; choose init, sync, track, check, apply, snapshot, packages, service, install (aliases: status, verify, coverage, managed, config, home, bootstrap, doctor)", topic)
	}
	return nil
}

func parseInitArgs(args []string) (bool, error) {
	if len(args) == 0 {
		return true, nil
	}
	if len(args) == 1 && args[0] == "--no-install" {
		return false, nil
	}
	return false, fmt.Errorf("usage: archstate init [--no-install]")
}

func parseInstallArgs(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if len(args) == 1 && args[0] == "--add-to-path" {
		return true, nil
	}
	return false, fmt.Errorf("usage: archstate install [--add-to-path]")
}

func (r *Runner) runInit(install bool) error {
	repo, err := r.discoverRepo()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(repo.path, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(repo.configDir(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(repo.homeDir(), 0o755); err != nil {
		return err
	}
	if err := createFileIfMissing(repo.markerPath(), []byte("archstate repository\n")); err != nil {
		return err
	}
	if err := createFileIfMissing(repo.pacmanPath(), formatState(nil)); err != nil {
		return err
	}
	if err := createFileIfMissing(repo.aurPath(), formatState(nil)); err != nil {
		return err
	}
	if err := createFileIfMissing(repo.packagesIgnorePath(), formatIgnoreList(nil)); err != nil {
		return err
	}
	if err := createFileIfMissing(repo.configPath(), formatState(nil)); err != nil {
		return err
	}
	if err := createFileIfMissing(repo.homePath(), formatState(nil)); err != nil {
		return err
	}
	fmt.Fprintf(r.Stdout, "initialized archstate repo at %s\n", repo.path)
	if install {
		return r.runInstall(false)
	}
	return nil
}

func (r *Runner) runSync(commit bool) error {
	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}

	var result packageSyncResult
	if err := r.withRepoLock(repo, "sync", func() error {
		var syncErr error
		result, syncErr = r.syncPackageState(repo)
		if syncErr != nil {
			return syncErr
		}
		if commit && !result.AlreadyCurrent {
			committed, commitErr := r.commitPackageState(repo)
			if commitErr != nil {
				return commitErr
			}
			result.Committed = committed
		}
		return nil
	}); err != nil {
		return err
	}
	r.printPackageSyncResult(result)
	return nil
}

type packageSyncResult struct {
	NativeCount    int
	AURCount       int
	AlreadyCurrent bool
	Committed      bool
}

// commitPackageState commits pacman.conf and aur.conf when the repo is a git
// worktree. It commits only the package-state files and only if they actually
// changed, so it never makes an empty commit or sweeps up unrelated edits. No-op
// without git.
func (r *Runner) commitPackageState(repo repoPaths) (bool, error) {
	if _, ok, err := repo.gitDir(); err != nil {
		return false, err
	} else if !ok {
		return false, nil
	}
	files := []string{pacmanConfFile, aurConfFile}
	if _, err := r.commandOutput("git", append([]string{"-C", repo.path, "add", "--"}, files...)...); err != nil {
		return false, err
	}
	out, err := r.commandOutput("git", append([]string{"-C", repo.path, "status", "--porcelain", "--"}, files...)...)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(out) == "" {
		return false, nil
	}
	msg := fmt.Sprintf("archstate sync %s", r.currentTime().Format(snapshotDisplayLayout))
	if _, err := r.commandOutput("git", append([]string{"-C", repo.path, "commit", "-m", msg, "--"}, files...)...); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Runner) syncPackageState(repo repoPaths) (packageSyncResult, error) {
	ignored, err := r.loadPackageIgnoreSet(repo)
	if err != nil {
		return packageSyncResult{}, err
	}
	native, err := r.queryPackageNames("pacman", "-Qqen")
	if err != nil {
		return packageSyncResult{}, err
	}
	foreign, err := r.queryPackageNames("pacman", "-Qqem")
	if err != nil {
		return packageSyncResult{}, err
	}
	native = filterIgnoredNames(native, ignored)
	foreign = filterIgnoredNames(foreign, ignored)
	result := packageSyncResult{
		NativeCount: len(native),
		AURCount:    len(foreign),
	}

	existingNative := filterIgnoredState(readPackageStateForSync(repo.pacmanPath()), ignored)
	existingForeign := filterIgnoredState(readPackageStateForSync(repo.aurPath()), ignored)
	if packageStateIsCurrent(repo.pacmanPath(), native, existingNative) &&
		packageStateIsCurrent(repo.aurPath(), foreign, existingForeign) {
		result.AlreadyCurrent = true
		return result, nil
	}

	allNames := append(append([]string{}, native...), foreign...)
	descriptions, err := r.queryPackageDescriptions(allNames)
	if err != nil {
		return packageSyncResult{}, err
	}

	nativeState := buildPackageState(native, existingNative, descriptions)
	foreignState := buildPackageState(foreign, existingForeign, descriptions)
	if _, err := r.createAutoSnapshot(repo); err != nil {
		return packageSyncResult{}, err
	}
	if err := writeStateFile(repo.pacmanPath(), nativeState); err != nil {
		return packageSyncResult{}, err
	}
	if err := writeStateFile(repo.aurPath(), foreignState); err != nil {
		return packageSyncResult{}, err
	}
	return result, nil
}

func (r *Runner) printPackageSyncResult(result packageSyncResult) {
	if result.AlreadyCurrent {
		fmt.Fprintf(r.Stdout, "already synced %d native and %d AUR packages\n", result.NativeCount, result.AURCount)
		return
	}
	if result.Committed {
		fmt.Fprintf(r.Stdout, "synced and committed %d native and %d AUR packages\n", result.NativeCount, result.AURCount)
		return
	}
	fmt.Fprintf(r.Stdout, "synced %d native and %d AUR packages\n", result.NativeCount, result.AURCount)
}

func parseSyncArgs(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if len(args) == 1 && args[0] == "--commit" {
		return true, nil
	}
	return false, fmt.Errorf("usage: archstate sync [--commit]")
}

func (r *Runner) runPackages(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		return r.printCommandHelp("packages")
	}
	if len(args) != 0 {
		return fmt.Errorf("usage: archstate packages")
	}

	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}
	if r.packageRemovalTUI == nil && !interactiveTerminal(r.Stdin, r.Stdout) {
		return fmt.Errorf("archstate packages requires an interactive terminal; run it directly in a terminal, not through a pipe or in a script")
	}

	return r.withRepoLock(repo, "packages", func() error {
		if _, err := r.syncPackageState(repo); err != nil {
			return fmt.Errorf("pre-sync failed: %w", err)
		}

		inventory, err := loadPackageRemovalInventory(repo)
		if err != nil {
			return err
		}
		if inventory.Empty() {
			fmt.Fprintln(r.Stdout, "no explicit packages to remove")
			return nil
		}

		selected, err := r.selectPackagesForRemoval(inventory)
		if err != nil {
			return err
		}
		if len(selected) == 0 {
			fmt.Fprintln(r.Stdout, "no packages selected")
			return nil
		}

		if err := r.removePackages(selected); err != nil {
			return err
		}
		if _, err := r.syncPackageState(repo); err != nil {
			return fmt.Errorf("post-removal sync failed: %w", err)
		}
		fmt.Fprintf(r.Stdout, "removed %d packages and synced package state\n", len(selected))
		return nil
	})
}

// checkRenamedOverwriteFlag turns the removed --overwrite flag into a clear
// pointer to its replacement instead of the flag package's generic "not defined"
// error, so anyone with the old muscle memory knows exactly what to type.
func checkRenamedOverwriteFlag(args []string) error {
	for _, arg := range args {
		if arg == "--overwrite" || arg == "-overwrite" ||
			strings.HasPrefix(arg, "--overwrite=") || strings.HasPrefix(arg, "-overwrite=") {
			return errors.New("the --overwrite flag was renamed to --restore")
		}
	}
	return nil
}

func (r *Runner) runApply(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		return r.printCommandHelp("apply")
	}
	return r.runBootstrapWithVerb("apply", args)
}

func (r *Runner) runBootstrap(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		return r.printCommandHelp("bootstrap")
	}
	return r.runBootstrapWithVerb("bootstrap", args)
}

func (r *Runner) runBootstrapWithVerb(verb string, args []string) error {
	if err := checkRenamedOverwriteFlag(args); err != nil {
		return err
	}
	fs := flag.NewFlagSet(verb, flag.ContinueOnError)
	fs.SetOutput(r.Stderr)
	var opts BootstrapOptions
	fs.BoolVar(&opts.DryRun, "dry-run", false, "show planned changes without applying them")
	fs.BoolVar(&opts.DotFiles, "dotfiles", false, "apply only config/home symlinks; skip packages (no sudo)")
	fs.BoolVar(&opts.Packages, "packages", false, "install only packages; skip config/home symlinks")
	fs.BoolVar(&opts.Adopt, "adopt", false, "save existing config/home conflicts into Archstate, then symlink")
	fs.BoolVar(&opts.Restore, "restore", false, "install tracked Archstate copies over config/home conflicts")
	fs.BoolVar(&opts.ForceSensitive, "force-sensitive", false, "allow adopting sensitive names denied by default")
	fs.StringVar(&opts.AURHelper, "aur-helper", "", "choose AUR helper to use or bootstrap: paru or yay")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: archstate %s [--dry-run] [--dotfiles|--packages] [--adopt|--restore] [--force-sensitive] [--aur-helper paru|yay]", verb)
	}
	if opts.Adopt && opts.Restore {
		return errors.New("--adopt and --restore are mutually exclusive")
	}
	if opts.DotFiles && opts.Packages {
		return errors.New("--dotfiles and --packages are mutually exclusive: one skips packages, the other skips config/home")
	}
	if opts.Packages && (opts.Adopt || opts.Restore) {
		return errors.New("--packages skips config/home, so --adopt and --restore have no effect")
	}
	if opts.ForceSensitive && !opts.Adopt {
		return errors.New("--force-sensitive only applies with --adopt")
	}
	if opts.DotFiles && opts.AURHelper != "" {
		return errors.New("--dotfiles skips packages, so --aur-helper has no effect")
	}
	if opts.AURHelper != "" && !isSupportedAURHelper(opts.AURHelper) {
		return fmt.Errorf("unsupported AUR helper %q; choose paru or yay", opts.AURHelper)
	}

	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}
	if opts.DryRun {
		plan, err := r.buildBootstrapPlan(repo, opts)
		if err != nil {
			return err
		}
		r.printBootstrapPlan(plan, opts)
		return nil
	}
	return r.withRepoLock(repo, verb, func() error {
		plan, err := r.buildBootstrapPlan(repo, opts)
		if err != nil {
			return err
		}
		return r.applyBootstrapPlan(plan, opts)
	})
}

func (r *Runner) runTrack(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		return r.printCommandHelp("track")
	}
	// Bare track or "track untrack" opens the managed untrack TUI.
	if len(args) == 0 {
		return r.runManagedAs("track", nil)
	}
	if args[0] == "untrack" {
		return r.runManagedAs("track untrack", args[1:])
	}
	switch args[0] {
	case "config":
		return r.runConfigAs("track config", args[1:])
	case "home":
		return r.runHomeAs("track home", args[1:])
	default:
		return fmt.Errorf("usage: archstate track [untrack|config|home] ...\n   or: archstate track config add|list|preview|rm ...\n   or: archstate track home add|list|preview|rm ...")
	}
}

func (r *Runner) runConfig(args []string) error {
	return r.runConfigAs("config", args)
}

func (r *Runner) runConfigAs(prefix string, args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		if strings.HasPrefix(prefix, "track") {
			return r.printCommandHelp("track")
		}
		return r.printCommandHelp("config")
	}
	if len(args) < 1 {
		return fmt.Errorf("usage: archstate %s add <name>\n   or: archstate %s list\n   or: archstate %s preview\n   or: archstate %s rm <name>", prefix, prefix, prefix, prefix)
	}
	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: archstate %s add <name>...", prefix)
		}
		return r.runConfigAdd(repo, args[1:])
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate %s list", prefix)
		}
		return r.runConfigList(repo)
	case "preview":
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate %s preview", prefix)
		}
		return r.runConfigPreview(repo)
	case "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: archstate %s rm <name>...", prefix)
		}
		return r.runConfigRemove(repo, args[1:])
	default:
		helpTopic := "config"
		if strings.HasPrefix(prefix, "track") {
			helpTopic = "track"
		}
		return fmt.Errorf("unknown %s command %q; expected add, list, preview, or rm (see 'archstate help %s')", prefix, args[0], helpTopic)
	}
}

func (r *Runner) runHome(args []string) error {
	return r.runHomeAs("home", args)
}

func (r *Runner) runHomeAs(prefix string, args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		if strings.HasPrefix(prefix, "track") {
			return r.printCommandHelp("track")
		}
		return r.printCommandHelp("home")
	}
	if len(args) < 1 {
		return fmt.Errorf("usage: archstate %s add <name>\n   or: archstate %s list\n   or: archstate %s preview\n   or: archstate %s rm <name>", prefix, prefix, prefix, prefix)
	}
	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: archstate %s add <name>...", prefix)
		}
		return r.runHomeAdd(repo, args[1:])
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate %s list", prefix)
		}
		return r.runHomeList(repo)
	case "preview":
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate %s preview", prefix)
		}
		return r.runHomePreview(repo)
	case "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: archstate %s rm <name>...", prefix)
		}
		return r.runHomeRemove(repo, args[1:])
	default:
		helpTopic := "home"
		if strings.HasPrefix(prefix, "track") {
			helpTopic = "track"
		}
		return fmt.Errorf("unknown %s command %q; expected add, list, preview, or rm (see 'archstate help %s')", prefix, args[0], helpTopic)
	}
}

func createFileIfMissing(path string, data []byte) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return atomicWriteFile(path, data, 0o644)
}

func (r *Runner) configDir() string {
	return filepath.Join(r.Home, ".config")
}
