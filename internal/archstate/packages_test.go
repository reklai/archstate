package archstate

import (
	"strings"
	"testing"
)

func TestQueryPackageNamesTreatsEmptyForeignListAsEmpty(t *testing.T) {
	env := newTestEnv(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqem)
    exit 1
    ;;
  *)
    echo "unexpected pacman args: $*" >&2
    exit 2
    ;;
esac
`)

	names, err := env.r.queryPackageNames("pacman", "-Qqem")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Fatalf("got packages %v, want none", names)
	}
}

func TestQueryPackageNamesReportsForeignQueryErrors(t *testing.T) {
	env := newTestEnv(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqem)
    echo "pacman database is locked" >&2
    exit 1
    ;;
  *)
    echo "unexpected pacman args: $*" >&2
    exit 2
    ;;
esac
`)

	_, err := env.r.queryPackageNames("pacman", "-Qqem")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "pacman -Qqem failed") || !strings.Contains(got, "pacman database is locked") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParsePacmanInfoFoldsWrappedDescriptions(t *testing.T) {
	// firefox's Description wraps onto a continuation line; git has a wrapped
	// Groups field before its (single-line) Description, which must not bleed in.
	out := `Name            : firefox
Version         : 1.0.0-1
Description     : A web browser built for speed, simplicity,
                  and safety on the modern web
Architecture    : x86_64

Name            : git
Groups          : base-devel
                  vcs
Description     : the fast distributed version control system
Installed Size  : 40.00 MiB
`
	got := parsePacmanInfo(out)
	want := map[string]string{
		"firefox": "A web browser built for speed, simplicity, and safety on the modern web",
		"git":     "the fast distributed version control system",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %v", len(got), len(want), got)
	}
	for name, desc := range want {
		if got[name] != desc {
			t.Errorf("description for %q = %q, want %q", name, got[name], desc)
		}
	}
}
