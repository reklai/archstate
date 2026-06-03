package archstate

import (
	"fmt"
	"os"
	"path/filepath"
)

const markerFile = ".archstate-root"

type repoPaths struct {
	path string
	home string
}

func (r *Runner) discoverRepo() (repoPaths, error) {
	if r.Cwd == "" {
		return repoPaths{}, fmt.Errorf("current directory is unknown")
	}
	if r.Home == "" {
		return repoPaths{}, fmt.Errorf("home directory is unknown")
	}
	start, err := filepath.Abs(r.Cwd)
	if err != nil {
		return repoPaths{}, err
	}
	for dir := start; ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(dir, markerFile)); err == nil {
			return repoPaths{path: dir, home: r.Home}, nil
		} else if err != nil && !os.IsNotExist(err) {
			return repoPaths{}, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return repoPaths{path: filepath.Join(r.Home, ".config", "archstate"), home: r.Home}, nil
}

func (r *Runner) discoverExistingRepo() (repoPaths, error) {
	repo, err := r.discoverRepo()
	if err != nil {
		return repoPaths{}, err
	}
	if err := requireInitialized(repo); err != nil {
		return repoPaths{}, err
	}
	return repo, nil
}

func requireInitialized(repo repoPaths) error {
	if _, err := os.Lstat(repo.markerPath()); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("repo is not initialized at %s; run archstate init", repo.path)
		}
		return err
	}
	return nil
}

func (r repoPaths) markerPath() string {
	return filepath.Join(r.path, markerFile)
}

func (r repoPaths) pacmanPath() string {
	return filepath.Join(r.path, "pacman.conf")
}

func (r repoPaths) aurPath() string {
	return filepath.Join(r.path, "aur.conf")
}

func (r repoPaths) configPath() string {
	return filepath.Join(r.path, "config.conf")
}

func (r repoPaths) homePath() string {
	return filepath.Join(r.path, "home.conf")
}

func (r repoPaths) configDir() string {
	return filepath.Join(r.path, "config")
}

func (r repoPaths) homeDir() string {
	return filepath.Join(r.path, "home")
}

func (r repoPaths) snapshotsDir() string {
	return filepath.Join(r.path, ".snapshots")
}

func (r repoPaths) snapshotPath(id string) string {
	return filepath.Join(r.snapshotsDir(), id)
}

func (r repoPaths) repoConfig(name string) string {
	return filepath.Join(r.configDir(), name)
}

func (r repoPaths) repoHome(name string) string {
	return filepath.Join(r.homeDir(), name)
}

func (r repoPaths) localConfig(name string) string {
	return filepath.Join(r.home, ".config", name)
}

func (r repoPaths) localHome(name string) string {
	return filepath.Join(r.home, name)
}
