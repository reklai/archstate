package archstate

import (
	"fmt"
	"os"
	"strings"
)

func (r *Runner) withRepoLock(repo repoPaths, op string, fn func() error) error {
	lockPath, err := repo.lockPath()
	if err != nil {
		return err
	}
	file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("repo is locked by another archstate command: %s\nif no archstate command is running, this lock is stale; delete the file above and retry", lockPath)
		}
		return err
	}
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(lockPath)
		}
	}()
	fmt.Fprintf(file, "%s\n", op)
	if err := file.Close(); err != nil {
		return err
	}
	if err := fn(); err != nil {
		return err
	}
	if err := os.Remove(lockPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (r *Runner) requireCleanGitRepo(repo repoPaths, op string) error {
	if _, ok, err := repo.gitDir(); err != nil {
		return err
	} else if !ok {
		return nil
	}
	out, err := r.commandOutput("git", "-C", repo.path, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("cannot check git status before %s: %w", op, err)
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	return fmt.Errorf("repo has uncommitted changes; commit or stash them before archstate %s", op)
}
