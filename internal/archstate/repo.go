package archstate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultRepoDirName = "archstate-src"
	markerFile         = ".archstate-root"
)

// Repo-state component names. snapshotStateNames must list exactly these
// (minus the marker); TestSnapshotStateNamesCoversRepoState enforces it.
const (
	pacmanConfFile = "pacman.conf"
	aurConfFile    = "aur.conf"
	configConfFile = "config.conf"
	homeConfFile   = "home.conf"
	configDirName  = "config"
	homeDirName    = "home"
)

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
	return repoPaths{path: filepath.Join(r.Home, ".config", defaultRepoDirName), home: r.Home}, nil
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
	return filepath.Join(r.path, pacmanConfFile)
}

func (r repoPaths) aurPath() string {
	return filepath.Join(r.path, aurConfFile)
}

func (r repoPaths) configPath() string {
	return filepath.Join(r.path, configConfFile)
}

func (r repoPaths) homePath() string {
	return filepath.Join(r.path, homeConfFile)
}

func (r repoPaths) configDir() string {
	return filepath.Join(r.path, configDirName)
}

func (r repoPaths) homeDir() string {
	return filepath.Join(r.path, homeDirName)
}

func (r repoPaths) snapshotsDir() string {
	return filepath.Join(r.path, ".snapshots")
}

func (r repoPaths) snapshotPath(id string) string {
	return filepath.Join(r.snapshotsDir(), id)
}

func (r repoPaths) gitDir() (string, bool, error) {
	gitPath := filepath.Join(r.path, ".git")
	info, err := os.Lstat(gitPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if info.IsDir() {
		return gitPath, true, nil
	}
	if info.Mode().IsRegular() {
		data, err := os.ReadFile(gitPath)
		if err != nil {
			return "", false, err
		}
		value := strings.TrimSpace(string(data))
		const prefix = "gitdir:"
		if !strings.HasPrefix(value, prefix) {
			return "", false, fmt.Errorf("unsupported .git file at %s", gitPath)
		}
		dir := strings.TrimSpace(strings.TrimPrefix(value, prefix))
		if dir == "" {
			return "", false, fmt.Errorf("empty gitdir in %s", gitPath)
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(r.path, dir)
		}
		return filepath.Clean(dir), true, nil
	}
	return "", false, fmt.Errorf("unsupported .git entry at %s", gitPath)
}

func (r repoPaths) lockPath() (string, error) {
	if gitDir, ok, err := r.gitDir(); err != nil {
		return "", err
	} else if ok {
		return filepath.Join(gitDir, "archstate.lock"), nil
	}
	return filepath.Join(r.path, ".archstate.lock"), nil
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
