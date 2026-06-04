package archstate

import "testing"

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
