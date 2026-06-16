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
		return r.runPackages(args[1:])
	case "status":
		if len(args) == 2 && isHelpArg(args[1]) {
			return r.printCommandHelp("status")
		}
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate status")
		}
		return r.runStatus()
	case "bootstrap":
		return r.runBootstrap(args[1:])
	case "config":
		return r.runConfig(args[1:])
	case "home":
		return r.runHome(args[1:])
	case "snapshot":
		return r.runSnapshot(args[1:])
	case "service":
		return r.runService(args[1:])
	case "doctor":
		if len(args) == 2 && isHelpArg(args[1]) {
			return r.printCommandHelp("doctor")
		}
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate doctor")
		}
		return r.runDoctor()
	default:
		return fmt.Errorf("unknown command %q", args[0])
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
  archstate config add nvim
  archstate home add .zshrc
  archstate snapshot save baseline
  archstate status
  archstate bootstrap --dry-run
  archstate bootstrap

Commands:
  init       Create repo state and install archstate to ~/.local/bin.
  install    Install or update archstate in ~/.local/bin.
  sync       Rewrite package state from explicit pacman/AUR packages.
  packages   Fuzzy-select explicit packages to remove.
  status     Show tracked state vs current machine drift.
  config     Manage direct children of ~/.config.
  home       Manage direct children of ~.
  snapshot   Save, list, restore, or remove repo-state snapshots.
  bootstrap  Install missing packages and recreate managed symlinks.
  doctor     Diagnose repo health and print concrete fix commands.
  service    Manage the optional systemd user sync timer.

Command help:
  archstate help <command>
  archstate <command> --help

Examples:
  archstate config add nvim
  archstate snapshot list --manual
  archstate bootstrap --dry-run`)
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

Open an interactive package removal TUI.

Behavior:
  sync package state before opening the TUI
  fuzzy-search Native or AUR packages
  mark packages for removal
  review the marked packages, then run one sudo pacman -Rns command
  sync package state again after successful removal

Keys:
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
	case "status":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate status

Show drift without changing anything.

Reports:
  tracked native/AUR packages that are missing
  explicitly installed native/AUR packages that are not tracked
  managed config and home entries as ok, missing, conflict, or error`)
	case "config":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate config add <name>...
  archstate config list
  archstate config preview
  archstate config rm <name>...

Manage direct children of ~/.config. add and rm accept multiple names.

Commands:
  add <name>  Save ~/.config/<name> into Archstate config/ and replace it with a symlink.
  list        Show currently tracked config entries.
  preview     Show ~/.config entries and which ones can be added.
  rm <name>   Stop managing ~/.config/<name>, restore it locally, and remove the saved copy.

Examples:
  archstate config add nvim kitty ghostty
  archstate config preview
  archstate config rm nvim`)
	case "home":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate home add <name>...
  archstate home list
  archstate home preview
  archstate home rm <name>...

Manage direct children of ~, such as shell/session files. add and rm accept multiple names.

Commands:
  add <name>  Save ~/<name> into Archstate home/ and replace it with a symlink.
  list        Show currently tracked home entries.
  preview     Show ~ dotfiles and which ones can be added.
  rm <name>   Stop managing ~/<name>, restore it locally, and remove the saved copy.

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
	case "bootstrap":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate bootstrap --dry-run
  archstate bootstrap [--adopt|--overwrite] [--aur-helper paru|yay]
  archstate bootstrap --dotfiles [--adopt|--overwrite]

Install missing packages and create managed config/home symlinks.

Options:
  --dry-run              Show planned installs, symlinks, conflicts, adoptions, or overwrites.
  --dotfiles             Apply only config/home symlinks; skip packages (needs no sudo or pacman).
  --aur-helper paru|yay  Use the selected AUR helper. If missing, bootstrap the matching helper.
  --adopt                Save unmanaged local config/home entries into Archstate, then symlink.
  --overwrite            Restore tracked Archstate entries over unmanaged local files.

Conflict behavior:
  Naked bootstrap fails on unmanaged config/home conflicts.
  --adopt works whether the tracked copy exists or not.
  --overwrite fails if the tracked copy is missing.

Examples:
  archstate bootstrap --dry-run
  archstate bootstrap --dotfiles --overwrite
  archstate bootstrap --aur-helper paru
  archstate bootstrap --adopt
  archstate bootstrap --overwrite`)
	case "doctor":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate doctor

Validate repo discovery, required commands, config parseability, package access,
AUR helper availability, package drift, and managed symlink health.

Output convention:
  OK     Healthy checks.
  WARN   Drift or incomplete information that does not block the repo.
  ERROR  Problems that need a fix before Archstate can be trusted.

Doctor prints exact next commands when a fix is known.`)
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
		return fmt.Errorf("unknown help topic %q; choose init, install, sync, packages, status, config, home, snapshot, bootstrap, doctor, or service", topic)
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
	native, err := r.queryPackageNames("pacman", "-Qqen")
	if err != nil {
		return packageSyncResult{}, err
	}
	foreign, err := r.queryPackageNames("pacman", "-Qqem")
	if err != nil {
		return packageSyncResult{}, err
	}
	result := packageSyncResult{
		NativeCount: len(native),
		AURCount:    len(foreign),
	}

	existingNative := readPackageStateForSync(repo.pacmanPath())
	existingForeign := readPackageStateForSync(repo.aurPath())
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
		return fmt.Errorf("archstate packages requires an interactive terminal")
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

func (r *Runner) runBootstrap(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		return r.printCommandHelp("bootstrap")
	}
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	fs.SetOutput(r.Stderr)
	var opts BootstrapOptions
	fs.BoolVar(&opts.DryRun, "dry-run", false, "show planned changes without applying them")
	fs.BoolVar(&opts.DotFiles, "dotfiles", false, "apply only config/home symlinks; skip packages (no sudo)")
	fs.BoolVar(&opts.Adopt, "adopt", false, "save existing .config conflicts into Archstate")
	fs.BoolVar(&opts.Overwrite, "overwrite", false, "restore tracked Archstate config over .config conflicts")
	fs.StringVar(&opts.AURHelper, "aur-helper", "", "choose AUR helper to use or bootstrap: paru or yay")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: archstate bootstrap [--dry-run] [--dotfiles] [--adopt|--overwrite] [--aur-helper paru|yay]")
	}
	if opts.Adopt && opts.Overwrite {
		return errors.New("--adopt and --overwrite are mutually exclusive")
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
	return r.withRepoLock(repo, "bootstrap", func() error {
		plan, err := r.buildBootstrapPlan(repo, opts)
		if err != nil {
			return err
		}
		return r.applyBootstrapPlan(plan, opts)
	})
}

func (r *Runner) runConfig(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		return r.printCommandHelp("config")
	}
	if len(args) < 1 {
		return fmt.Errorf("usage: archstate config add <name>\n   or: archstate config list\n   or: archstate config preview\n   or: archstate config rm <name>")
	}
	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: archstate config add <name>...")
		}
		return r.runConfigAdd(repo, args[1:])
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate config list")
		}
		return r.runConfigList(repo)
	case "preview":
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate config preview")
		}
		return r.runConfigPreview(repo)
	case "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: archstate config rm <name>...")
		}
		return r.runConfigRemove(repo, args[1:])
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

func (r *Runner) runHome(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		return r.printCommandHelp("home")
	}
	if len(args) < 1 {
		return fmt.Errorf("usage: archstate home add <name>\n   or: archstate home list\n   or: archstate home preview\n   or: archstate home rm <name>")
	}
	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: archstate home add <name>...")
		}
		return r.runHomeAdd(repo, args[1:])
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate home list")
		}
		return r.runHomeList(repo)
	case "preview":
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate home preview")
		}
		return r.runHomePreview(repo)
	case "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: archstate home rm <name>...")
		}
		return r.runHomeRemove(repo, args[1:])
	default:
		return fmt.Errorf("unknown home command %q", args[0])
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
