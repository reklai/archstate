package archstate

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func firstLineOf(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// TestPackageRemovalViewFitsTerminalWhenScrolled guards the bug where scrolling
// the package list grew the view past the terminal height (rows were 2 columns
// too wide for the panel's text area and wrapped), which scrolled the search bar
// off the top of the alt-screen. The view must never exceed the terminal height,
// and the search bar must stay on the top line, at every scroll position.
func TestPackageRemovalViewFitsTerminalWhenScrolled(t *testing.T) {
	var inv packageRemovalInventory
	for i := range 80 {
		inv.Native = append(inv.Native, packageRemovalItem{
			Name:        fmt.Sprintf("native-package-%03d", i),
			Description: "a description long enough to fill the entire available row width",
			Kind:        packageRemovalNative,
		})
	}
	for i := range 40 {
		inv.AUR = append(inv.AUR, packageRemovalItem{
			Name:        fmt.Sprintf("aur-package-%03d", i),
			Description: "another long aur package description that fills the row width",
			Kind:        packageRemovalAUR,
		})
	}

	sizes := []struct{ w, h int }{{80, 24}, {100, 30}, {120, 50}, {200, 40}}
	for _, size := range sizes {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			var m tea.Model = newPackageRemovalModel(inv)
			m, _ = m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})

			assertFits := func(label string) {
				t.Helper()
				view := m.(packageRemovalModel).View()
				if got := lipgloss.Height(view); got > size.h {
					t.Fatalf("%s: view height %d exceeds terminal height %d", label, got, size.h)
				}
				if !strings.Contains(firstLineOf(view), "Search") {
					t.Fatalf("%s: search bar is not on the top line:\n%s", label, view)
				}
			}

			assertFits("initial")

			// Enter the package list (first Down moves focus out of search), then
			// scroll past the end of the native section.
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
			for i := 0; i < len(inv.Native)+5; i++ {
				m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
				assertFits("native scroll")
			}

			// Switch to the AUR section and scroll it to the bottom too.
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
			assertFits("aur top")
			for i := 0; i < len(inv.AUR)+5; i++ {
				m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
				assertFits("aur scroll")
			}
		})
	}
}

// TestPackageRemovalConfirmViewFitsTerminalWhenScrolled guards the review page:
// marking many packages and scrolling the confirmation must stay within the
// terminal height (the old plain-text confirm view could overflow), and the
// command preview must remain visible.
func TestPackageRemovalConfirmViewFitsTerminalWhenScrolled(t *testing.T) {
	var inv packageRemovalInventory
	for i := range 80 {
		inv.Native = append(inv.Native, packageRemovalItem{Name: fmt.Sprintf("native-%03d", i), Kind: packageRemovalNative})
	}
	for i := range 40 {
		inv.AUR = append(inv.AUR, packageRemovalItem{Name: fmt.Sprintf("aur-%03d", i), Kind: packageRemovalAUR})
	}
	for _, size := range []struct{ w, h int }{{80, 24}, {100, 30}, {120, 50}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			start := newPackageRemovalModel(inv)
			for _, item := range append(append([]packageRemovalItem{}, inv.Native...), inv.AUR...) {
				start.marked[item.Name] = item
			}
			start.confirming = true
			var m tea.Model = start
			m, _ = m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})

			assertFits := func(label string) {
				t.Helper()
				view := m.(packageRemovalModel).View()
				if got := lipgloss.Height(view); got > size.h {
					t.Fatalf("%s: confirm view height %d exceeds terminal height %d", label, got, size.h)
				}
				if !strings.Contains(view, "Command:") {
					t.Fatalf("%s: confirm view missing command preview:\n%s", label, view)
				}
			}

			assertFits("initial")
			for i := 0; i < len(inv.Native)+len(inv.AUR)+5; i++ {
				m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
				assertFits("scroll")
			}
		})
	}
}

func TestPackageRemovalConfirmSpaceUnmarksAndExitsWhenEmpty(t *testing.T) {
	inv := packageRemovalInventory{Native: []packageRemovalItem{
		{Name: "alpha", Kind: packageRemovalNative},
		{Name: "beta", Kind: packageRemovalNative},
	}}
	start := newPackageRemovalModel(inv)
	start.marked["alpha"] = inv.Native[0]
	start.marked["beta"] = inv.Native[1]
	start.confirming = true

	var m tea.Model = start
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	pm := m.(packageRemovalModel)
	if _, ok := pm.marked["alpha"]; ok {
		t.Fatalf("space should unmark the item under the cursor; marked=%v", pm.marked)
	}
	if !pm.confirming {
		t.Fatal("one package still marked: should stay on the review page")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	pm = m.(packageRemovalModel)
	if len(pm.marked) != 0 {
		t.Fatalf("expected all packages unmarked, got %v", pm.marked)
	}
	if pm.confirming {
		t.Fatal("unmarking the last package should leave the review page")
	}
}

func TestPackageRemovalConfirmViewShowsDescriptions(t *testing.T) {
	inv := packageRemovalInventory{
		Native: []packageRemovalItem{
			{Name: "git", Description: "the fast distributed version control system", Kind: packageRemovalNative},
		},
		AUR: []packageRemovalItem{
			{Name: "paru-bin", Description: "feature packed AUR helper", Kind: packageRemovalAUR},
		},
	}
	start := newPackageRemovalModel(inv)
	start.marked["git"] = inv.Native[0]
	start.marked["paru-bin"] = inv.AUR[0]
	start.confirming = true

	var m tea.Model = start
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	view := m.(packageRemovalModel).View()

	for _, want := range []string{
		"git - the fast distributed version control system", // native, on the cursor row
		"paru-bin",
		"feature packed AUR helper", // AUR description still shown
		"(AUR)",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("review view missing %q:\n%s", want, view)
		}
	}
}
