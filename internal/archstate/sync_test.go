package archstate

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncRewritesPackagesFromPacmanAndRepairsFiles(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
case "$1" in
  -Qqen)
    printf 'neovim\ngit\n'
    ;;
  -Qqem)
    printf 'paru-bin\n'
    ;;
  -Qi)
    shift
    for pkg in "$@"; do
      case "$pkg" in
        git) desc='the fast distributed version control system' ;;
        neovim) desc='vim-fork focused on extensibility and usability' ;;
        paru-bin) desc='feature packed AUR helper' ;;
        *) desc='' ;;
      esac
      printf 'Name            : %s\n' "$pkg"
      printf 'Description     : %s\n\n' "$desc"
    done
    ;;
  *)
    echo "unexpected pacman args: $*" >&2
    exit 2
    ;;
esac
`)
	writeFile(t, filepath.Join(env.repo, "pacman.conf"), generatedHeader+`
# removed by sync
git=custom git note
bad line
old=old description
`)

	if err := env.run("sync"); err != nil {
		t.Fatal(err)
	}

	pacman := readFile(t, filepath.Join(env.repo, "pacman.conf"))
	if !strings.HasPrefix(pacman, generatedHeader) {
		t.Fatalf("missing generated header:\n%s", pacman)
	}
	for _, want := range []string{
		"git=custom git note\n",
		"neovim=vim-fork focused on extensibility and usability\n",
	} {
		if !strings.Contains(pacman, want) {
			t.Fatalf("pacman.conf missing %q:\n%s", want, pacman)
		}
	}
	for _, unwanted := range []string{"bad line", "old=old description", "# removed by sync"} {
		if strings.Contains(pacman, unwanted) {
			t.Fatalf("pacman.conf still contains %q:\n%s", unwanted, pacman)
		}
	}

	aur := readFile(t, filepath.Join(env.repo, "aur.conf"))
	if !strings.Contains(aur, "paru-bin=feature packed AUR helper\n") {
		t.Fatalf("aur.conf missing AUR package:\n%s", aur)
	}
}
