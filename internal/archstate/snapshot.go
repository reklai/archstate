package archstate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	autoSnapshotLimit     = 5
	snapshotTimeLayout    = "2006-01-02_15-04-05"
	snapshotDisplayLayout = "2006/01/02-15:04:05"
)

type snapshotInfo struct {
	ID          string
	Kind        string
	Name        string
	Timestamp   string
	DisplayTime string
}

func (r *Runner) runSnapshot(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		return r.printCommandHelp("snapshot")
	}
	if len(args) < 1 {
		return snapshotUsage()
	}
	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}

	switch args[0] {
	case "save":
		if len(args) != 2 {
			return fmt.Errorf("usage: archstate snapshot save <name>")
		}
		var info snapshotInfo
		if err := r.withRepoLock(repo, "snapshot save", func() error {
			var err error
			info, err = r.createSnapshot(repo, "manual", args[1])
			return err
		}); err != nil {
			return err
		}
		fmt.Fprintf(r.Stdout, "saved snapshot %s  %s  %s\n", info.ID, info.DisplayTime, info.Name)
		return nil
	case "list":
		filter, err := parseSnapshotListFilter(args[1:])
		if err != nil {
			return err
		}
		return r.runSnapshotList(repo, filter)
	case "restore":
		if len(args) != 2 {
			return fmt.Errorf("usage: archstate snapshot restore <id>")
		}
		if err := r.withRepoLock(repo, "snapshot restore", func() error {
			if err := r.requireCleanGitRepo(repo, "snapshot restore"); err != nil {
				return err
			}
			if _, err := r.createAutoSnapshot(repo); err != nil {
				return err
			}
			return restoreSnapshot(repo, args[1])
		}); err != nil {
			return err
		}
		fmt.Fprintf(r.Stdout, "restored snapshot %s\n", args[1])
		return nil
	case "rm":
		if len(args) != 2 {
			return fmt.Errorf("usage: archstate snapshot rm <id>")
		}
		if err := r.withRepoLock(repo, "snapshot rm", func() error {
			return removeSnapshot(repo, args[1])
		}); err != nil {
			return err
		}
		fmt.Fprintf(r.Stdout, "removed snapshot %s\n", args[1])
		return nil
	default:
		return fmt.Errorf("unknown snapshot command %q", args[0])
	}
}

func snapshotUsage() error {
	return fmt.Errorf("usage: archstate snapshot save <name>\n   or: archstate snapshot list [--manual|--auto]\n   or: archstate snapshot restore <id>\n   or: archstate snapshot rm <id>")
}

func parseSnapshotListFilter(args []string) (string, error) {
	filter := ""
	for _, arg := range args {
		switch arg {
		case "--manual":
			if filter != "" {
				return "", fmt.Errorf("--manual and --auto are mutually exclusive")
			}
			filter = "manual"
		case "--auto":
			if filter != "" {
				return "", fmt.Errorf("--manual and --auto are mutually exclusive")
			}
			filter = "auto"
		default:
			return "", fmt.Errorf("usage: archstate snapshot list [--manual|--auto]")
		}
	}
	return filter, nil
}

func (r *Runner) runSnapshotList(repo repoPaths, filter string) error {
	snapshots, err := listSnapshots(repo)
	if err != nil {
		return err
	}
	snapshots = filterSnapshots(snapshots, filter)
	fmt.Fprintln(r.Stdout, "Snapshots:")
	if len(snapshots) == 0 {
		fmt.Fprintln(r.Stdout, "  none")
		return nil
	}
	fmt.Fprintln(r.Stdout, "  ID                                                      NAME              TYPE    TIME")
	for _, snapshot := range snapshots {
		fmt.Fprintf(r.Stdout, "  %-55s %-17s %-7s %s\n", snapshot.ID, snapshotListName(snapshot), snapshot.Kind, snapshot.DisplayTime)
	}
	return nil
}

func filterSnapshots(snapshots []snapshotInfo, filter string) []snapshotInfo {
	if filter == "" {
		return snapshots
	}
	filtered := make([]snapshotInfo, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.Kind == filter {
			filtered = append(filtered, snapshot)
		}
	}
	return filtered
}

func (r *Runner) createAutoSnapshot(repo repoPaths) (snapshotInfo, error) {
	info, err := r.createSnapshot(repo, "auto", "")
	if err != nil {
		return snapshotInfo{}, err
	}
	if err := pruneAutoSnapshots(repo, autoSnapshotLimit); err != nil {
		return snapshotInfo{}, err
	}
	return info, nil
}

func (r *Runner) createSnapshot(repo repoPaths, kind, name string) (snapshotInfo, error) {
	if kind != "auto" && kind != "manual" {
		return snapshotInfo{}, fmt.Errorf("invalid snapshot kind %q", kind)
	}
	if kind == "manual" {
		if err := validateSnapshotName(name); err != nil {
			return snapshotInfo{}, fmt.Errorf("invalid snapshot name %q: %w", name, err)
		}
	} else if name != "" {
		return snapshotInfo{}, fmt.Errorf("automatic snapshots must not have names")
	}
	if err := os.MkdirAll(repo.snapshotsDir(), 0o755); err != nil {
		return snapshotInfo{}, err
	}

	info, err := r.nextSnapshotInfo(repo, kind, name)
	if err != nil {
		return snapshotInfo{}, err
	}
	dst := repo.snapshotPath(info.ID)
	if err := os.Mkdir(dst, 0o755); err != nil {
		return snapshotInfo{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(dst)
		}
	}()

	if err := copySnapshotState(repo, dst); err != nil {
		return snapshotInfo{}, err
	}
	cleanup = false
	return info, nil
}

func (r *Runner) nextSnapshotInfo(repo repoPaths, kind, name string) (snapshotInfo, error) {
	stamp := r.currentTime().Format(snapshotTimeLayout)
	baseID := kind + "-" + stamp
	if kind == "manual" {
		baseID += "-" + name
	}
	for i := 0; i < 1000; i++ {
		id := baseID
		if i > 0 {
			// '+' is rejected by validateSnapshotName, so a collision counter
			// can never be mistaken for part of a manual snapshot's name.
			id = fmt.Sprintf("%s+%d", baseID, i+1)
		}
		if !pathExists(repo.snapshotPath(id)) {
			return parseSnapshotID(id)
		}
	}
	return snapshotInfo{}, fmt.Errorf("could not allocate unique snapshot id for %s", baseID)
}

func (r *Runner) currentTime() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// snapshotStateNames lists every component of repo state a snapshot captures.
// It must stay in sync with the files/dirs init creates; the constants are the
// single source of truth and TestSnapshotStateNamesCoversRepoState guards it.
func snapshotStateNames() []string {
	return []string{
		pacmanConfFile,
		aurConfFile,
		configConfFile,
		homeConfFile,
		configDirName,
		homeDirName,
	}
}

func copySnapshotState(repo repoPaths, dst string) error {
	for _, name := range snapshotStateNames() {
		src := filepath.Join(repo.path, name)
		if _, err := os.Lstat(src); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if err := copyPath(src, filepath.Join(dst, name)); err != nil {
			return err
		}
	}
	return nil
}

func restoreSnapshot(repo repoPaths, id string) error {
	if _, err := parseSnapshotID(id); err != nil {
		return err
	}
	src := repo.snapshotPath(id)
	info, err := os.Lstat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("snapshot not found: %s", id)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("snapshot is not a directory: %s", id)
	}

	stage, err := os.MkdirTemp(repo.path, ".archstate-snapshot-restore-")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stage)
		}
	}()

	for _, name := range snapshotStateNames() {
		snapshotPath := filepath.Join(src, name)
		if _, err := os.Lstat(snapshotPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if err := copyPath(snapshotPath, filepath.Join(stage, name)); err != nil {
			return err
		}
	}

	for _, name := range snapshotStateNames() {
		dst := filepath.Join(repo.path, name)
		staged := filepath.Join(stage, name)
		if _, err := os.Lstat(staged); err != nil {
			if os.IsNotExist(err) {
				if err := os.RemoveAll(dst); err != nil {
					return err
				}
				continue
			}
			return err
		}
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
		if err := os.Rename(staged, dst); err != nil {
			return err
		}
	}

	cleanup = false
	return os.RemoveAll(stage)
}

func removeSnapshot(repo repoPaths, id string) error {
	if _, err := parseSnapshotID(id); err != nil {
		return err
	}
	path := repo.snapshotPath(id)
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("snapshot not found: %s", id)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("snapshot is not a directory: %s", id)
	}
	return os.RemoveAll(path)
}

func listSnapshots(repo repoPaths) ([]snapshotInfo, error) {
	entries, err := os.ReadDir(repo.snapshotsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	snapshots := make([]snapshotInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := parseSnapshotID(entry.Name())
		if err != nil {
			continue
		}
		snapshots = append(snapshots, info)
	}
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].Timestamp == snapshots[j].Timestamp {
			return snapshots[i].ID > snapshots[j].ID
		}
		return snapshots[i].Timestamp > snapshots[j].Timestamp
	})
	return snapshots, nil
}

func pruneAutoSnapshots(repo repoPaths, keep int) error {
	snapshots, err := listSnapshots(repo)
	if err != nil {
		return err
	}
	auto := make([]snapshotInfo, 0)
	for _, snapshot := range snapshots {
		if snapshot.Kind == "auto" {
			auto = append(auto, snapshot)
		}
	}
	if len(auto) <= keep {
		return nil
	}
	sort.Slice(auto, func(i, j int) bool {
		if auto[i].Timestamp == auto[j].Timestamp {
			return auto[i].ID < auto[j].ID
		}
		return auto[i].Timestamp < auto[j].Timestamp
	})
	for _, snapshot := range auto[:len(auto)-keep] {
		if err := os.RemoveAll(repo.snapshotPath(snapshot.ID)); err != nil {
			return err
		}
	}
	return nil
}

func parseSnapshotID(id string) (snapshotInfo, error) {
	if id == "" || id == "." || id == ".." {
		return snapshotInfo{}, fmt.Errorf("invalid snapshot id %q", id)
	}
	if strings.ContainsAny(id, `/\`) || strings.ContainsRune(id, 0) {
		return snapshotInfo{}, fmt.Errorf("invalid snapshot id %q", id)
	}

	kind := ""
	stampStart := 0
	nameRequired := false
	switch {
	case strings.HasPrefix(id, "auto-"):
		kind = "auto"
		stampStart = len("auto-")
	case strings.HasPrefix(id, "manual-"):
		kind = "manual"
		stampStart = len("manual-")
		nameRequired = true
	default:
		return snapshotInfo{}, fmt.Errorf("invalid snapshot id %q", id)
	}

	if len(id) < stampStart+len(snapshotTimeLayout) {
		return snapshotInfo{}, fmt.Errorf("invalid snapshot id %q", id)
	}
	stamp := id[stampStart : stampStart+len(snapshotTimeLayout)]
	parsed, err := time.Parse(snapshotTimeLayout, stamp)
	if err != nil {
		return snapshotInfo{}, fmt.Errorf("invalid snapshot id %q", id)
	}
	if parsed.Format(snapshotTimeLayout) != stamp {
		return snapshotInfo{}, fmt.Errorf("invalid snapshot id %q", id)
	}
	name := ""
	suffix := id[stampStart+len(snapshotTimeLayout):]
	// Strip an optional collision counter "+N" (decimal). '+' is not a legal
	// snapshot-name character, so the counter is unambiguous for both kinds and
	// a manual snapshot keeps its original name after a same-second collision.
	if i := strings.IndexByte(suffix, '+'); i >= 0 {
		if !isDecimal(suffix[i+1:]) {
			return snapshotInfo{}, fmt.Errorf("invalid snapshot id %q", id)
		}
		suffix = suffix[:i]
	}
	if nameRequired {
		if !strings.HasPrefix(suffix, "-") || len(suffix) == 1 {
			return snapshotInfo{}, fmt.Errorf("invalid snapshot id %q", id)
		}
		name = suffix[1:]
		if err := validateSnapshotName(name); err != nil {
			return snapshotInfo{}, fmt.Errorf("invalid snapshot id %q", id)
		}
	} else if suffix != "" {
		return snapshotInfo{}, fmt.Errorf("invalid snapshot id %q", id)
	}

	return snapshotInfo{
		ID:          id,
		Kind:        kind,
		Name:        name,
		Timestamp:   stamp,
		DisplayTime: parsed.Format(snapshotDisplayLayout),
	}, nil
}

func snapshotListName(snapshot snapshotInfo) string {
	if snapshot.Name == "" {
		return "-"
	}
	return snapshot.Name
}

func validateSnapshotName(name string) error {
	if err := validateDirectChildName(name); err != nil {
		return err
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '.', '_', '-':
			continue
		default:
			return fmt.Errorf("must contain only letters, numbers, '.', '_' or '-'")
		}
	}
	return nil
}

func isDecimal(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
