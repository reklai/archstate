package archstate

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sensitiveDenyFile is an optional repo file listing extra direct-child names
// that must not be adopted without --force-sensitive.
const sensitiveDenyFile = "sensitive.deny"

// defaultSensitiveNames are high-risk home/.config entries that almost never
// belong in a reproducibility repo without an explicit force.
var defaultSensitiveNames = []string{
	".ssh",
	".gnupg",
	".password-store",
	".aws",
	".kube",
	".docker",
	".netrc",
	".npmrc",
	".pypirc",
	".git-credentials",
	"gcloud",
	"gh",
	"op",
	"Bitwarden",
	"keepassxc",
	"kdewallet",
}

func (r repoPaths) sensitiveDenyPath() string {
	return filepath.Join(r.path, sensitiveDenyFile)
}

func readSensitiveDenyFile(path string) (map[string]bool, error) {
	denied := make(map[string]bool)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return denied, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := validateDirectChildName(line); err != nil {
			return nil, fmt.Errorf("%s:%d: invalid sensitive name %q: %w", path, lineNo, line, err)
		}
		denied[line] = true
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return denied, nil
}

func loadSensitiveDeny(repo repoPaths) (map[string]bool, error) {
	denied, err := readSensitiveDenyFile(repo.sensitiveDenyPath())
	if err != nil {
		return nil, err
	}
	for _, name := range defaultSensitiveNames {
		denied[name] = true
	}
	return denied, nil
}

func checkSensitiveName(repo repoPaths, name string, force bool) error {
	if force {
		return nil
	}
	denied, err := loadSensitiveDeny(repo)
	if err != nil {
		return err
	}
	if !denied[name] {
		return nil
	}
	return fmt.Errorf("%q looks sensitive and is denied by default; pass --force-sensitive if you really want to track it in Archstate", name)
}

func isSensitiveName(repo repoPaths, name string) bool {
	denied, err := loadSensitiveDeny(repo)
	if err != nil {
		// Fail closed in preview: if deny file is broken, still treat defaults as sensitive.
		for _, d := range defaultSensitiveNames {
			if name == d {
				return true
			}
		}
		return false
	}
	return denied[name]
}
