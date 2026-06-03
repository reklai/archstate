package archstate

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate install")
		}
		return r.runInstall()
	case "sync":
		if len(args) == 2 && isHelpArg(args[1]) {
			return r.printCommandHelp("sync")
		}
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate sync")
		}
		return r.runSync()
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
  ~/.config/archstate

Common workflow:
  archstate init
  archstate sync
  archstate config add nvim
  archstate home add .zshrc
  archstate snapshot save baseline
  archstate status
  archstate bootstrap --preview
  archstate bootstrap

Commands:
  init       Create repo state and install archstate to ~/.local/bin.
  install    Install or update archstate in ~/.local/bin.
  sync       Rewrite package state from explicit pacman/AUR packages.
  status     Show tracked state vs current machine drift.
  config     Manage direct children of ~/.config.
  home       Manage direct children of ~.
  snapshot   Save, list, restore, or remove repo-state snapshots.
  bootstrap  Install missing packages and recreate managed symlinks.
  doctor     Diagnose repo health and print concrete fix commands.

Command help:
  archstate help <command>
  archstate <command> --help

Examples:
  archstate config add nvim
  archstate snapshot list --manual
  archstate bootstrap --preview`)
}

func isHelpArg(arg string) bool {
	return arg == "--help" || arg == "-h"
}

func (r *Runner) printCommandHelp(topic string) error {
	switch topic {
	case "init":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate init [--no-install]

Create ~/.config/archstate, state files, config/home directories, and install
this archstate binary to ~/.local/bin/archstate.

Options:
  --no-install  Create repo state without installing the binary.

Examples:
  archstate init
  archstate init --no-install`)
	case "install":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate install

Install or update this archstate binary at ~/.local/bin/archstate.
If ~/.local/bin is not in PATH, print the shell config line to add.
Archstate does not edit shell files automatically.`)
	case "sync":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate sync

Rewrite package state from this machine's explicit packages.

Sources:
  pacman -Qqen  -> pacman.conf
  pacman -Qqem  -> aur.conf

Notes:
  Existing package descriptions are preserved by package name.
  Malformed package-file lines, comments, and blanks are cleaned up.
  If package files already match this machine, sync does not snapshot or rewrite.
  An automatic snapshot is created before package files are rewritten.`)
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
  archstate config add <name>
  archstate config rm <name>

Manage direct children of ~/.config.

Commands:
  add <name>  Save ~/.config/<name> into Archstate config/ and replace it with a symlink.
  rm <name>   Stop managing ~/.config/<name>, restore it locally, and remove the saved copy.

Examples:
  archstate config add nvim
  archstate config rm nvim`)
	case "home":
		fmt.Fprintln(r.Stdout, `Usage:
  archstate home add <name>
  archstate home rm <name>

Manage direct children of ~, such as shell/session files.

Commands:
  add <name>  Save ~/<name> into Archstate home/ and replace it with a symlink.
  rm <name>   Stop managing ~/<name>, restore it locally, and remove the saved copy.

Examples:
  archstate home add .zshrc
  archstate home add .profile
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
  archstate bootstrap --preview
  archstate bootstrap [--adopt|--overwrite] [--aur-helper paru|yay]

Install missing packages and create managed config/home symlinks.

Options:
  --preview              Show planned installs, symlinks, conflicts, adoptions, or overwrites.
  --aur-helper paru|yay  Use the selected AUR helper. If missing, bootstrap the matching helper.
  --adopt                Save unmanaged local config/home entries into Archstate, then symlink.
  --overwrite            Restore tracked Archstate entries over unmanaged local files.

Conflict behavior:
  Naked bootstrap fails on unmanaged config/home conflicts.
  --adopt works whether the tracked copy exists or not.
  --overwrite fails if the tracked copy is missing.

Examples:
  archstate bootstrap --preview
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
	default:
		return fmt.Errorf("unknown help topic %q; choose init, install, sync, status, config, home, snapshot, bootstrap, or doctor", topic)
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
		return r.runInstall()
	}
	return nil
}

func (r *Runner) runSync() error {
	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}

	native, err := r.queryPackageNames("pacman", "-Qqen")
	if err != nil {
		return err
	}
	foreign, err := r.queryPackageNames("pacman", "-Qqem")
	if err != nil {
		return err
	}

	existingNative := readPackageStateForSync(repo.pacmanPath())
	existingForeign := readPackageStateForSync(repo.aurPath())
	if packageStateIsCurrent(repo.pacmanPath(), native, existingNative) &&
		packageStateIsCurrent(repo.aurPath(), foreign, existingForeign) {
		fmt.Fprintf(r.Stdout, "already synced %d native and %d AUR packages\n", len(native), len(foreign))
		return nil
	}

	allNames := append(append([]string{}, native...), foreign...)
	descriptions, err := r.queryPackageDescriptions(allNames)
	if err != nil {
		return err
	}

	nativeState := buildPackageState(native, existingNative, descriptions)
	foreignState := buildPackageState(foreign, existingForeign, descriptions)

	if _, err := r.createAutoSnapshot(repo); err != nil {
		return err
	}
	if err := writeStateFile(repo.pacmanPath(), nativeState); err != nil {
		return err
	}
	if err := writeStateFile(repo.aurPath(), foreignState); err != nil {
		return err
	}
	fmt.Fprintf(r.Stdout, "synced %d native and %d AUR packages\n", len(nativeState), len(foreignState))
	return nil
}

func (r *Runner) runBootstrap(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		return r.printCommandHelp("bootstrap")
	}
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	fs.SetOutput(r.Stderr)
	var opts BootstrapOptions
	fs.BoolVar(&opts.Preview, "preview", false, "show planned changes without applying them")
	fs.BoolVar(&opts.Adopt, "adopt", false, "save existing .config conflicts into Archstate")
	fs.BoolVar(&opts.Overwrite, "overwrite", false, "restore tracked Archstate config over .config conflicts")
	fs.StringVar(&opts.AURHelper, "aur-helper", "", "choose AUR helper to use or bootstrap: paru or yay")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: archstate bootstrap [--preview] [--adopt|--overwrite] [--aur-helper paru|yay]")
	}
	if opts.Adopt && opts.Overwrite {
		return errors.New("--adopt and --overwrite are mutually exclusive")
	}
	if opts.AURHelper != "" && !isSupportedAURHelper(opts.AURHelper) {
		return fmt.Errorf("unsupported AUR helper %q; choose paru or yay", opts.AURHelper)
	}

	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}
	plan, err := r.buildBootstrapPlan(repo, opts)
	if err != nil {
		return err
	}
	if opts.Preview {
		r.printBootstrapPlan(plan)
		return nil
	}
	return r.applyBootstrapPlan(plan, opts)
}

func (r *Runner) runConfig(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		return r.printCommandHelp("config")
	}
	if len(args) < 1 {
		return fmt.Errorf("usage: archstate config add <name>\n   or: archstate config rm <name>")
	}
	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		if len(args) != 2 {
			return fmt.Errorf("usage: archstate config add <name>")
		}
		return r.runConfigAdd(repo, args[1])
	case "rm":
		if len(args) != 2 {
			return fmt.Errorf("usage: archstate config rm <name>")
		}
		return r.runConfigRemove(repo, args[1])
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

func (r *Runner) runHome(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		return r.printCommandHelp("home")
	}
	if len(args) < 1 {
		return fmt.Errorf("usage: archstate home add <name>\n   or: archstate home rm <name>")
	}
	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		if len(args) != 2 {
			return fmt.Errorf("usage: archstate home add <name>")
		}
		return r.runHomeAdd(repo, args[1])
	case "rm":
		if len(args) != 2 {
			return fmt.Errorf("usage: archstate home rm <name>")
		}
		return r.runHomeRemove(repo, args[1])
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
