package archstate

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestPackageRemovalModelLaunchesInSearchAndFuzzyFilters(t *testing.T) {
	model := newPackageRemovalModel(testPackageRemovalInventory())
	if model.focus != packageRemovalFocusSearch {
		t.Fatalf("initial focus = %v, want search", model.focus)
	}

	model = updatePackageRemovalRunes(t, model, "nvim")

	if got, want := packageRemovalNames(model.visiblePackages()), []string{"neovim"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("visible packages = %v, want %v", got, want)
	}
}

func TestPackageRemovalViewUsesInventoryTabsWithoutGlobalTitle(t *testing.T) {
	model := newPackageRemovalModel(testPackageRemovalInventory())

	view := model.View()
	if strings.Contains(view, "Archstate packages") {
		t.Fatalf("view should not include old global title:\n%s", view)
	}
	for _, want := range []string{"Search:", "(1) Native", "(2) AUR", "Marked"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "(1) Native") < strings.Index(view, "Marked") {
		t.Fatalf("section tabs should be in the inventory pane, after marked pane header:\n%s", view)
	}
}

func TestPackageRemovalFooterHintsAreCentered(t *testing.T) {
	model := newPackageRemovalModel(testPackageRemovalInventory())
	model.width = 100

	view := model.View()
	lines := strings.Split(view, "\n")
	footer := lines[len(lines)-1]
	if !strings.Contains(footer, "space= mark") || !strings.Contains(footer, " | ") {
		t.Fatalf("footer missing instruction hints:\n%s", view)
	}
	if !strings.HasPrefix(footer, " ") {
		t.Fatalf("footer hints should be centered with leading space: %q", footer)
	}
}

func TestPackageRemovalPackagePaneScrollsWithCursor(t *testing.T) {
	inventory := packageRemovalInventory{}
	for i := range 12 {
		inventory.Native = append(inventory.Native, packageRemovalItem{
			Name: fmt.Sprintf("pkg%02d", i),
			Kind: packageRemovalNative,
		})
	}
	model := newPackageRemovalModel(inventory)
	model.focus = packageRemovalFocusPackages
	model.packageCursor[int(packageRemovalSectionNative)] = 8

	view := model.packagePaneView(50, 8)
	if strings.Contains(view, "pkg00") {
		t.Fatalf("package pane did not scroll past first item:\n%s", view)
	}
	if !strings.Contains(view, "> [ ] pkg08") {
		t.Fatalf("package pane does not show cursor item after scrolling:\n%s", view)
	}
}

func TestPackageRemovalResizePersistsVisibleScrollState(t *testing.T) {
	inventory := packageRemovalInventory{}
	for i := range 60 {
		inventory.Native = append(inventory.Native, packageRemovalItem{
			Name: fmt.Sprintf("pkg%02d", i),
			Kind: packageRemovalNative,
		})
	}
	model := newPackageRemovalModel(inventory)
	model.focus = packageRemovalFocusPackages
	model.height = 30
	model.packageCursor[int(packageRemovalSectionNative)] = 40
	model.packageOffset[int(packageRemovalSectionNative)] = 20

	updated, cmd := model.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	if cmd != nil {
		t.Fatal("resize should not quit")
	}
	resized := updated.(packageRemovalModel)
	if got := resized.packageCursor[int(packageRemovalSectionNative)]; got != 40 {
		t.Fatalf("cursor after resize = %d, want 40", got)
	}
	if got, want := resized.packageOffset[int(packageRemovalSectionNative)], 37; got != want {
		t.Fatalf("offset after resize = %d, want %d", got, want)
	}

	leftWidth, _, _ := resized.layout()
	updated, cmd = resized.Update(tea.MouseMsg{
		X:      leftWidth + 3,
		Y:      5,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	if cmd != nil {
		t.Fatal("wheel should not quit")
	}
	scrolled := updated.(packageRemovalModel)
	if got := scrolled.packageCursor[int(packageRemovalSectionNative)]; got != 40 {
		t.Fatalf("cursor after wheel = %d, want 40", got)
	}
	if got := scrolled.packageOffset[int(packageRemovalSectionNative)]; got != 40 {
		t.Fatalf("offset after wheel = %d, want 40", got)
	}
}

func TestPackageRemovalHighlightsFuzzyMatchCharacters(t *testing.T) {
	indexes, ok := fuzzyPackageMatchIndexes("neovim", "nvm")
	if !ok || !reflect.DeepEqual(indexes, []int{0, 3, 5}) {
		t.Fatalf("match indexes = %v, %v; want [0 3 5], true", indexes, ok)
	}

	model := newPackageRemovalModel(testPackageRemovalInventory())
	model.query = "nvm"
	highlighted := model.highlightPackageName("neovim")
	if lipgloss.Width(highlighted) != len("neovim") {
		t.Fatalf("highlighted name should preserve display width: %q", highlighted)
	}
}

func TestPackageRemovalFocusedRowsAreHighlighted(t *testing.T) {
	highlighted := packageRemovalRow("> [ ] git", 20, true)

	if !strings.Contains(highlighted, "> [ ] git") {
		t.Fatalf("highlighted row should retain row text: %q", highlighted)
	}
	if got := lipgloss.Width(highlighted); got != 20 {
		t.Fatalf("highlighted row width = %d, want 20: %q", got, highlighted)
	}
}

func TestPackageRemovalRowsStayWithinWidthWhenTruncated(t *testing.T) {
	longPlain := packageRemovalRow("> [ ] "+strings.Repeat("a", 80), 20, false)
	if got := lipgloss.Width(longPlain); got != 20 {
		t.Fatalf("plain truncated row width = %d, want 20: %q", got, longPlain)
	}

	longHighlighted := packageRemovalRow("> [ ] "+packageRemovalMatchStyle.Render("a")+strings.Repeat("b", 80), 20, true)
	if got := lipgloss.Width(longHighlighted); got != 20 {
		t.Fatalf("highlighted truncated row width = %d, want 20: %q", got, longHighlighted)
	}
}

func TestPackageRemovalViewKeepsSearchWhenLastPackageVisible(t *testing.T) {
	inventory := packageRemovalInventory{}
	for i := range 30 {
		description := "short"
		if i == 29 {
			description = strings.Repeat("long description ", 12)
		}
		inventory.Native = append(inventory.Native, packageRemovalItem{
			Name:        fmt.Sprintf("pkg%02d", i),
			Description: description,
			Kind:        packageRemovalNative,
		})
	}
	model := newPackageRemovalModel(inventory)
	model.width = 80
	model.height = 12
	model.focus = packageRemovalFocusPackages
	model.packageCursor[int(packageRemovalSectionNative)] = len(inventory.Native) - 1
	model.ensurePackageCursorVisible(packageRemovalSectionNative)

	view := model.View()
	lines := strings.Split(view, "\n")
	if len(lines) > model.height {
		t.Fatalf("view height = %d, want <= %d:\n%s", len(lines), model.height, view)
	}
	if !strings.Contains(lines[0], "Search:") {
		t.Fatalf("search row should remain first line when last package is visible:\n%s", view)
	}
	if !strings.Contains(view, "pkg29") {
		t.Fatalf("last package should be visible after scrolling:\n%s", view)
	}
}

func TestPackageRemovalSearchFocusIsObvious(t *testing.T) {
	model := newPackageRemovalModel(testPackageRemovalInventory())
	if got := model.searchView(40); !strings.Contains(got, "Search:") || strings.Contains(got, "> Search:") || lipgloss.Width(got) != 40 {
		t.Fatalf("focused search should use label without cursor marker: %q", got)
	}
	if got := model.searchView(40); strings.Contains(got, "\x1b[") {
		t.Fatalf("focused empty search should not contain nested ANSI resets: %q", got)
	}

	model.focus = packageRemovalFocusPackages
	got := model.searchView(40)
	if strings.Contains(got, "> Search:") {
		t.Fatalf("unfocused search should not use cursor prefix: %q", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("unfocused search should be unstyled: %q", got)
	}
}

func TestPackageRemovalSearchIsCentered(t *testing.T) {
	model := newPackageRemovalModel(testPackageRemovalInventory())
	model.query = "git"

	got := model.searchView(30)
	if lipgloss.Width(got) != 30 {
		t.Fatalf("centered search width = %d, want 30: %q", lipgloss.Width(got), got)
	}
	if !strings.HasPrefix(got, "       ") {
		t.Fatalf("search row should be centered with leading space: %q", got)
	}
}

func TestPackageRemovalQSearchesFromSearchFocus(t *testing.T) {
	model := newPackageRemovalModel(testPackageRemovalInventory())

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd != nil {
		t.Fatal("q from search should update query, not quit")
	}
	next := updated.(packageRemovalModel)
	if next.query != "q" {
		t.Fatalf("query after q = %q, want q", next.query)
	}
}

func TestPackageRemovalEscExitsOutsideConfirmation(t *testing.T) {
	for _, focus := range []packageRemovalFocus{
		packageRemovalFocusSearch,
		packageRemovalFocusPackages,
		packageRemovalFocusMarked,
	} {
		model := newPackageRemovalModel(testPackageRemovalInventory())
		model.focus = focus
		model.query = "git"
		model.marked["git"] = model.inventory.Native[0]

		_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if cmd == nil {
			t.Fatalf("esc with focus %v should quit", focus)
		}
	}
}

func TestPackageRemovalEscCancelsConfirmation(t *testing.T) {
	model := newPackageRemovalModel(testPackageRemovalInventory())
	model.confirming = true

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("esc in confirmation should cancel, not quit")
	}
	next := updated.(packageRemovalModel)
	if next.confirming {
		t.Fatal("esc in confirmation should close confirmation")
	}
}

func TestPackageRemovalMouseClickFocusesSearch(t *testing.T) {
	model := newPackageRemovalModel(testPackageRemovalInventory())
	model.focus = packageRemovalFocusPackages

	updated, cmd := model.Update(tea.MouseMsg{
		X:      10,
		Y:      0,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if cmd != nil {
		t.Fatal("mouse click should not quit")
	}
	next := updated.(packageRemovalModel)
	if next.focus != packageRemovalFocusSearch {
		t.Fatalf("mouse click on search focus = %v, want search", next.focus)
	}
}

func TestPackageRemovalMouseClickMovesPackageCursor(t *testing.T) {
	model := newPackageRemovalModel(testPackageRemovalInventory())
	model.focus = packageRemovalFocusSearch
	leftWidth, _, _ := model.layout()

	updated, cmd := model.Update(tea.MouseMsg{
		X:      leftWidth + 3,
		Y:      4,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if cmd != nil {
		t.Fatal("mouse click should not quit")
	}
	next := updated.(packageRemovalModel)
	if next.focus != packageRemovalFocusPackages {
		t.Fatalf("mouse click on package list focus = %v, want packages", next.focus)
	}
	if got := next.packageCursor[int(packageRemovalSectionNative)]; got != 0 {
		t.Fatalf("package cursor after click = %d, want 0", got)
	}
	if len(next.marked) != 0 {
		t.Fatalf("single click should focus/move only, not mark: %v", next.marked)
	}
}

func TestPackageRemovalMouseClickFocusedPackageListDoesNotMoveCursor(t *testing.T) {
	model := newPackageRemovalModel(testPackageRemovalInventory())
	model.focus = packageRemovalFocusPackages
	model.packageCursor[int(packageRemovalSectionNative)] = 0
	leftWidth, _, _ := model.layout()

	updated, cmd := model.Update(tea.MouseMsg{
		X:      leftWidth + 3,
		Y:      5,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if cmd != nil {
		t.Fatal("mouse click should not quit")
	}
	next := updated.(packageRemovalModel)
	if got := next.packageCursor[int(packageRemovalSectionNative)]; got != 0 {
		t.Fatalf("focused package cursor after single click = %d, want 0", got)
	}
	if len(next.marked) != 0 {
		t.Fatalf("focused single click should not mark packages: %v", next.marked)
	}
}

func TestPackageRemovalMouseDoubleClickMarksPackage(t *testing.T) {
	model := newPackageRemovalModel(testPackageRemovalInventory())
	leftWidth, _, _ := model.layout()
	click := tea.MouseMsg{
		X:      leftWidth + 3,
		Y:      5,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}
	release := click
	release.Action = tea.MouseActionRelease

	updated, _ := model.Update(click)
	updated, _ = updated.(packageRemovalModel).Update(release)
	updated, cmd := updated.(packageRemovalModel).Update(click)
	if cmd != nil {
		t.Fatal("mouse double click should not quit")
	}
	next := updated.(packageRemovalModel)
	if got := packageRemovalNames(next.selectedItems()); !reflect.DeepEqual(got, []string{"neovim"}) {
		t.Fatalf("selected after double click = %v, want [neovim]", got)
	}
}

func TestPackageRemovalMouseDoubleClickFocusedPackageListMarksClickedPackage(t *testing.T) {
	model := newPackageRemovalModel(testPackageRemovalInventory())
	model.focus = packageRemovalFocusPackages
	model.packageCursor[int(packageRemovalSectionNative)] = 0
	leftWidth, _, _ := model.layout()
	click := tea.MouseMsg{
		X:      leftWidth + 3,
		Y:      5,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}
	release := click
	release.Action = tea.MouseActionRelease

	updated, _ := model.Update(click)
	first := updated.(packageRemovalModel)
	if got := first.packageCursor[int(packageRemovalSectionNative)]; got != 0 {
		t.Fatalf("focused package cursor after first click = %d, want 0", got)
	}
	updated, _ = first.Update(release)
	updated, cmd := updated.(packageRemovalModel).Update(click)
	if cmd != nil {
		t.Fatal("mouse double click should not quit")
	}
	next := updated.(packageRemovalModel)
	if got := packageRemovalNames(next.selectedItems()); !reflect.DeepEqual(got, []string{"neovim"}) {
		t.Fatalf("selected after focused double click = %v, want [neovim]", got)
	}
	if got := next.packageCursor[int(packageRemovalSectionNative)]; got != 1 {
		t.Fatalf("focused package cursor after double click = %d, want 1", got)
	}
}

func TestPackageRemovalMouseDoubleClickDoesNotShiftWhenWindowMoves(t *testing.T) {
	inventory := packageRemovalInventory{}
	for i := range 12 {
		inventory.Native = append(inventory.Native, packageRemovalItem{
			Name: fmt.Sprintf("pkg%02d", i),
			Kind: packageRemovalNative,
		})
	}
	model := newPackageRemovalModel(inventory)
	model.height = 12
	model.packageCursor[int(packageRemovalSectionNative)] = 4
	model.packageOffset[int(packageRemovalSectionNative)] = 4
	leftWidth, _, _ := model.layout()
	click := tea.MouseMsg{
		X:      leftWidth + 3,
		Y:      6,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}
	release := click
	release.Action = tea.MouseActionRelease

	updated, _ := model.Update(click)
	first := updated.(packageRemovalModel)
	clickedName := first.visiblePackages()[first.packageCursor[int(packageRemovalSectionNative)]].Name
	updated, _ = first.Update(release)
	updated, cmd := updated.(packageRemovalModel).Update(click)
	if cmd != nil {
		t.Fatal("mouse double click should not quit")
	}
	next := updated.(packageRemovalModel)
	if got := packageRemovalNames(next.selectedItems()); !reflect.DeepEqual(got, []string{clickedName}) {
		t.Fatalf("selected after double click = %v, want [%s]", got, clickedName)
	}
}

func TestPackageRemovalMouseWheelScrollsPackageList(t *testing.T) {
	inventory := packageRemovalInventory{}
	for i := range 12 {
		inventory.Native = append(inventory.Native, packageRemovalItem{
			Name: fmt.Sprintf("pkg%02d", i),
			Kind: packageRemovalNative,
		})
	}
	model := newPackageRemovalModel(inventory)
	model.height = 12
	leftWidth, _, _ := model.layout()

	updated, cmd := model.Update(tea.MouseMsg{
		X:      leftWidth + 3,
		Y:      5,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	if cmd != nil {
		t.Fatal("mouse wheel should not quit")
	}
	next := updated.(packageRemovalModel)
	if next.focus != packageRemovalFocusPackages {
		t.Fatalf("mouse wheel focus = %v, want packages", next.focus)
	}
	if got := next.packageOffset[int(packageRemovalSectionNative)]; got != 3 {
		t.Fatalf("package offset after wheel = %d, want 3", got)
	}
	if got := next.packageCursor[int(packageRemovalSectionNative)]; got != 3 {
		t.Fatalf("package cursor after wheel = %d, want 3", got)
	}
}

func TestPackageRemovalMouseClickSwitchesSectionTabs(t *testing.T) {
	model := newPackageRemovalModel(testPackageRemovalInventory())
	leftWidth, _, _ := model.layout()
	nativeText := packageRemovalTabText(1, "Native", len(model.inventory.Native), true)

	updated, cmd := model.Update(tea.MouseMsg{
		X:      leftWidth + 3 + lipgloss.Width(nativeText) + 1,
		Y:      2,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if cmd != nil {
		t.Fatal("mouse click should not quit")
	}
	next := updated.(packageRemovalModel)
	if next.active != packageRemovalSectionAUR {
		t.Fatalf("active section after tab click = %v, want AUR", next.active)
	}
}

func TestPackageRemovalModelTabCyclesBetweenPackageAndMarkedLists(t *testing.T) {
	model := newPackageRemovalModel(testPackageRemovalInventory())

	model = updatePackageRemovalKey(t, model, "tab")
	if model.focus != packageRemovalFocusPackages {
		t.Fatalf("tab from search focus = %v, want package list", model.focus)
	}
	model = updatePackageRemovalKey(t, model, "tab")
	if model.focus != packageRemovalFocusPackages {
		t.Fatalf("tab with no marked packages should stay in package list, got %v", model.focus)
	}
	model = updatePackageRemovalKey(t, model, " ")
	model = updatePackageRemovalKey(t, model, "tab")
	if model.focus != packageRemovalFocusMarked {
		t.Fatalf("tab from package focus = %v, want marked list", model.focus)
	}
	model = updatePackageRemovalKey(t, model, "tab")
	if model.focus != packageRemovalFocusPackages {
		t.Fatalf("tab from marked focus = %v, want package list", model.focus)
	}
	model = updatePackageRemovalKey(t, model, "/")
	if model.focus != packageRemovalFocusSearch {
		t.Fatalf("/ focus = %v, want search", model.focus)
	}
}

func TestPackageRemovalModelMarksFromBothPanesAndConfirms(t *testing.T) {
	model := newPackageRemovalModel(testPackageRemovalInventory())
	model = updatePackageRemovalKey(t, model, "tab")
	model = updatePackageRemovalKey(t, model, " ")
	if got := packageRemovalNames(model.selectedItems()); !reflect.DeepEqual(got, []string{"git"}) {
		t.Fatalf("selected after package mark = %v, want [git]", got)
	}

	model = updatePackageRemovalKey(t, model, "tab")
	model = updatePackageRemovalKey(t, model, " ")
	if got := packageRemovalNames(model.selectedItems()); len(got) != 0 {
		t.Fatalf("selected after marked-pane unmark = %v, want none", got)
	}

	model = updatePackageRemovalKey(t, model, "tab")
	model = updatePackageRemovalKey(t, model, " ")
	model = updatePackageRemovalKey(t, model, "2")
	model = updatePackageRemovalKey(t, model, " ")
	if got, want := packageRemovalNames(model.selectedItems()), []string{"git", "paru-bin"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected packages = %v, want %v", got, want)
	}

	model = updatePackageRemovalKey(t, model, "enter")
	if !model.confirming {
		t.Fatal("enter with marked packages should open confirmation")
	}
	model = updatePackageRemovalKey(t, model, "enter")
	if !model.confirmed {
		t.Fatal("enter in confirmation should confirm removal")
	}
}

func TestPackageRemovalCommandArgsUseOneDeterministicPacmanRemoval(t *testing.T) {
	items := []packageRemovalItem{
		{Name: "paru-bin", Kind: packageRemovalAUR},
		{Name: "git", Kind: packageRemovalNative},
		{Name: "git", Kind: packageRemovalNative},
	}

	got := packageRemovalCommandArgs(items)
	want := []string{"pacman", "-Rns", "git", "paru-bin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command args = %v, want %v", got, want)
	}
}

func TestUninstallSyncsRemovesWithPacmanAndSyncsAgain(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	statePath := filepath.Join(env.root, "package-state")
	logPath := filepath.Join(env.root, "remove.log")
	writeFile(t, statePath, "before\n")
	env.r.Env = append(env.r.Env,
		"ARCHSTATE_PACKAGE_STATE="+statePath,
		"ARCHSTATE_LOG="+logPath,
	)
	writePackageRemovalFakePacman(t, env)
	writeExecutable(t, filepath.Join(env.bin, "sudo"), `
echo "sudo $*" >> "$ARCHSTATE_LOG"
if [ "$*" != "pacman -Rns git paru-bin" ]; then
  echo "unexpected sudo args: $*" >&2
  exit 2
fi
printf 'after\n' > "$ARCHSTATE_PACKAGE_STATE"
`)
	env.r.packageRemovalTUI = func(inventory packageRemovalInventory) ([]packageRemovalItem, error) {
		if got, want := packageRemovalNames(inventory.Native), []string{"git", "neovim"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("native inventory = %v, want %v", got, want)
		}
		if got, want := packageRemovalNames(inventory.AUR), []string{"paru-bin"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("AUR inventory = %v, want %v", got, want)
		}
		return []packageRemovalItem{inventory.Native[0], inventory.AUR[0]}, nil
	}

	if err := env.run("uninstall"); err != nil {
		t.Fatal(err)
	}

	if got := strings.TrimSpace(readFile(t, logPath)); got != "sudo pacman -Rns git paru-bin" {
		t.Fatalf("remove log = %q", got)
	}
	pacman := readFile(t, filepath.Join(env.repo, "pacman.conf"))
	if strings.Contains(pacman, "git=") || !strings.Contains(pacman, "neovim=neovim desc\n") {
		t.Fatalf("post-sync pacman.conf did not match removed state:\n%s", pacman)
	}
	aur := readFile(t, filepath.Join(env.repo, "aur.conf"))
	if strings.Contains(aur, "paru-bin=") {
		t.Fatalf("post-sync aur.conf still contains removed package:\n%s", aur)
	}
	if !strings.Contains(env.stdout.String(), "removed 2 packages and synced package state") {
		t.Fatalf("missing success output:\n%s", env.stdout.String())
	}
}

func TestUninstallRemovalFailureSkipsPostSync(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	statePath := filepath.Join(env.root, "package-state")
	writeFile(t, statePath, "before\n")
	env.r.Env = append(env.r.Env, "ARCHSTATE_PACKAGE_STATE="+statePath)
	writePackageRemovalFakePacman(t, env)
	writeExecutable(t, filepath.Join(env.bin, "sudo"), `
printf 'after\n' > "$ARCHSTATE_PACKAGE_STATE"
exit 9
`)
	env.r.packageRemovalTUI = func(inventory packageRemovalInventory) ([]packageRemovalItem, error) {
		return []packageRemovalItem{inventory.Native[0]}, nil
	}

	err := env.run("uninstall")
	if err == nil {
		t.Fatal("expected removal failure")
	}
	if !strings.Contains(err.Error(), "sudo pacman -Rns git failed") {
		t.Fatalf("unexpected error: %v", err)
	}

	pacman := readFile(t, filepath.Join(env.repo, "pacman.conf"))
	if !strings.Contains(pacman, "git=git desc\n") {
		t.Fatalf("post-sync should not run after failed removal:\n%s", pacman)
	}
}

func TestUninstallNonTTYFailsBeforeSync(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	writeFakePacman(t, env.bin, `
echo "pacman should not run for non-TTY packages command" >&2
exit 3
`)

	err := env.run("uninstall")
	if err == nil {
		t.Fatal("expected non-TTY error")
	}
	if !strings.Contains(err.Error(), "requires an interactive terminal") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUninstallTUIErrorSkipsRemoval(t *testing.T) {
	env := newTestEnv(t)
	env.initRepo(t)
	env.r.Env = append(env.r.Env, "ARCHSTATE_PACKAGE_STATE="+filepath.Join(env.root, "package-state"))
	writeFile(t, filepath.Join(env.root, "package-state"), "before\n")
	writePackageRemovalFakePacman(t, env)
	writeExecutable(t, filepath.Join(env.bin, "sudo"), `
echo "sudo should not run" >&2
exit 3
`)
	wantErr := errors.New("tui failed")
	env.r.packageRemovalTUI = func(packageRemovalInventory) ([]packageRemovalItem, error) {
		return nil, wantErr
	}

	err := env.run("uninstall")
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func testPackageRemovalInventory() packageRemovalInventory {
	return packageRemovalInventory{
		Native: []packageRemovalItem{
			{Name: "git", Description: "git desc", Kind: packageRemovalNative},
			{Name: "neovim", Description: "neovim desc", Kind: packageRemovalNative},
			{Name: "ripgrep", Description: "ripgrep desc", Kind: packageRemovalNative},
		},
		AUR: []packageRemovalItem{
			{Name: "paru-bin", Description: "paru desc", Kind: packageRemovalAUR},
		},
	}
}

func updatePackageRemovalRunes(t *testing.T, model packageRemovalModel, runes string) packageRemovalModel {
	t.Helper()
	return updatePackageRemovalMsg(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(runes)})
}

func updatePackageRemovalKey(t *testing.T, model packageRemovalModel, key string) packageRemovalModel {
	t.Helper()
	switch key {
	case "tab":
		return updatePackageRemovalMsg(t, model, tea.KeyMsg{Type: tea.KeyTab})
	case "enter":
		return updatePackageRemovalMsg(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	case "esc":
		return updatePackageRemovalMsg(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	case "down":
		return updatePackageRemovalMsg(t, model, tea.KeyMsg{Type: tea.KeyDown})
	case "up":
		return updatePackageRemovalMsg(t, model, tea.KeyMsg{Type: tea.KeyUp})
	case "backspace":
		return updatePackageRemovalMsg(t, model, tea.KeyMsg{Type: tea.KeyBackspace})
	default:
		return updatePackageRemovalRunes(t, model, key)
	}
}

func updatePackageRemovalMsg(t *testing.T, model packageRemovalModel, msg tea.KeyMsg) packageRemovalModel {
	t.Helper()
	updated, _ := model.Update(msg)
	next, ok := updated.(packageRemovalModel)
	if !ok {
		t.Fatalf("updated model has type %T", updated)
	}
	return next
}

func packageRemovalNames(items []packageRemovalItem) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names
}

func writePackageRemovalFakePacman(t *testing.T, env *testEnv) {
	t.Helper()
	writeFakePacman(t, env.bin, `
state=before
if [ -r "$ARCHSTATE_PACKAGE_STATE" ]; then
  IFS= read -r state < "$ARCHSTATE_PACKAGE_STATE"
fi
case "$1" in
  -Qqen)
    if [ "$state" = before ]; then
      printf 'git\nneovim\n'
    else
      printf 'neovim\n'
    fi
    ;;
  -Qqem)
    if [ "$state" = before ]; then
      printf 'paru-bin\n'
    fi
    ;;
  -Qi)
    shift
    for pkg in "$@"; do
      case "$pkg" in
        git) desc='git desc' ;;
        neovim) desc='neovim desc' ;;
        paru-bin) desc='paru desc' ;;
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
}
