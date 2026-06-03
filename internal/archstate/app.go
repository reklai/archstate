package archstate

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Runner struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Cwd    string
	Home   string
	Env    []string
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
		r.printUsage()
		return nil
	}

	switch args[0] {
	case "help", "-h", "--help":
		r.printUsage()
		return nil
	case "init":
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate init")
		}
		return r.runInit()
	case "sync":
		if len(args) != 1 {
			return fmt.Errorf("usage: archstate sync")
		}
		return r.runSync()
	case "bootstrap":
		return r.runBootstrap(args[1:])
	case "dot":
		return r.runDot(args[1:])
	case "doctor":
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

func (r *Runner) printUsage() {
	fmt.Fprintln(r.Stdout, `Usage:
  archstate init
  archstate sync
  archstate bootstrap [--preview] [--adopt|--overwrite]
  archstate dot add <name>
  archstate dot rm <name>
  archstate doctor`)
}

func (r *Runner) runInit() error {
	repo, err := r.discoverRepo()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(repo.path, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(repo.dotfilesDir(), 0o755); err != nil {
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
	if err := createFileIfMissing(repo.dotfilesPath(), formatState(nil)); err != nil {
		return err
	}
	fmt.Fprintf(r.Stdout, "initialized archstate repo at %s\n", repo.path)
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
	allNames := append(append([]string{}, native...), foreign...)
	descriptions, err := r.queryPackageDescriptions(allNames)
	if err != nil {
		return err
	}

	nativeState := buildPackageState(native, existingNative, descriptions)
	foreignState := buildPackageState(foreign, existingForeign, descriptions)

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
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	fs.SetOutput(r.Stderr)
	var opts BootstrapOptions
	fs.BoolVar(&opts.Preview, "preview", false, "show planned changes without applying them")
	fs.BoolVar(&opts.Adopt, "adopt", false, "adopt unmanaged dotfile conflicts when possible")
	fs.BoolVar(&opts.Overwrite, "overwrite", false, "replace unmanaged dotfile conflicts with managed symlinks")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: archstate bootstrap [--preview] [--adopt|--overwrite]")
	}
	if opts.Adopt && opts.Overwrite {
		return errors.New("--adopt and --overwrite are mutually exclusive")
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

func (r *Runner) runDot(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: archstate dot add <name>\n   or: archstate dot rm <name>")
	}
	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		if len(args) != 2 {
			return fmt.Errorf("usage: archstate dot add <name>")
		}
		return r.runDotAdd(repo, args[1])
	case "rm":
		if len(args) != 2 {
			return fmt.Errorf("usage: archstate dot rm <name>")
		}
		return r.runDotRemove(repo, args[1])
	default:
		return fmt.Errorf("unknown dot command %q", args[0])
	}
}

func (r *Runner) runDoctor() error {
	repo, err := r.discoverRepo()
	if err != nil {
		return err
	}
	ok := true
	check := func(label string, err error) {
		if err != nil {
			ok = false
			fmt.Fprintf(r.Stdout, "ERROR %s: %v\n", label, err)
			return
		}
		fmt.Fprintf(r.Stdout, "OK %s\n", label)
	}

	check("repo", requireInitialized(repo))
	check("pacman command", r.requireCommand("pacman"))
	check("sudo command", r.requireCommand("sudo"))

	pacmanEntries, pacmanErr := readStateFileStrict(repo.pacmanPath(), validatePackageEntry)
	check("pacman.conf", pacmanErr)
	aurEntries, aurErr := readStateFileStrict(repo.aurPath(), validatePackageEntry)
	check("aur.conf", aurErr)
	dotEntries, dotErr := readStateFileStrict(repo.dotfilesPath(), validateDotfileEntry)
	check("dotfiles.conf", dotErr)

	if aurErr == nil && len(aurEntries) > 0 {
		if _, err := r.findAURHelper(); err != nil {
			check("AUR helper", err)
		} else {
			check("AUR helper", nil)
		}
	}
	if pacmanErr == nil || aurErr == nil {
		if _, err := r.queryPackageNames("pacman", "-Qq"); err != nil {
			check("package access", err)
		} else {
			check("package access", nil)
		}
	}
	if dotErr == nil {
		check("dotfile health", checkDotfileHealth(repo, dotEntries))
	}
	if pacmanErr == nil && aurErr == nil {
		_ = pacmanEntries
	}

	if !ok {
		return errors.New("doctor found problems")
	}
	return nil
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
