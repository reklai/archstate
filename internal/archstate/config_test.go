package archstate

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatStateWritesHeaderAndSortedEntries(t *testing.T) {
	got := string(formatState(map[string]string{
		"neovim": "vim-fork focused on extensibility and usability",
		"git":    "the fast distributed version control system",
	}))
	want := generatedHeader +
		"git=the fast distributed version control system\n" +
		"neovim=vim-fork focused on extensibility and usability\n"
	if got != want {
		t.Fatalf("formatState mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestStrictParserRejectsUnsupportedComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pacman.conf")
	writeFile(t, path, generatedHeader+"# user note\npkg=description\n")

	_, err := readStateFileStrict(path, validatePackageEntry)
	if err == nil {
		t.Fatal("expected strict parser error")
	}
	if !strings.Contains(err.Error(), "unsupported comment") {
		t.Fatalf("expected unsupported comment error, got %v", err)
	}
	if !strings.Contains(err.Error(), ":5:") {
		t.Fatalf("expected line-specific error, got %v", err)
	}
}

func TestSyncPackageParserSilentlyKeepsOnlyValidEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pacman.conf")
	writeFile(t, path, generatedHeader+`
# custom note
bad line
valid=kept
also-valid=second
bad/name=ignored
 spaced =ignored
`)

	entries := readPackageStateForSync(path)
	if len(entries) != 2 {
		t.Fatalf("expected 2 valid entries, got %#v", entries)
	}
	if entries["valid"] != "kept" || entries["also-valid"] != "second" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
}
