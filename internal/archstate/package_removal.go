package archstate

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-isatty"
)

type packageRemovalKind int

const (
	packageRemovalNative packageRemovalKind = iota
	packageRemovalAUR
)

type packageRemovalItem struct {
	Name        string
	Description string
	Kind        packageRemovalKind
}

type packageRemovalInventory struct {
	Native []packageRemovalItem
	AUR    []packageRemovalItem
}

type packageRemovalTUIFunc func(packageRemovalInventory) ([]packageRemovalItem, error)

func loadPackageRemovalInventory(repo repoPaths) (packageRemovalInventory, error) {
	nativeState, err := readStateFileStrictOptional(repo.pacmanPath(), validatePackageEntry)
	if err != nil {
		return packageRemovalInventory{}, err
	}
	aurState, err := readStateFileStrictOptional(repo.aurPath(), validatePackageEntry)
	if err != nil {
		return packageRemovalInventory{}, err
	}
	return packageRemovalInventory{
		Native: packageRemovalItemsFromState(nativeState, packageRemovalNative),
		AUR:    packageRemovalItemsFromState(aurState, packageRemovalAUR),
	}, nil
}

func packageRemovalItemsFromState(state map[string]string, kind packageRemovalKind) []packageRemovalItem {
	names := sortedEntryKeys(state)
	items := make([]packageRemovalItem, 0, len(names))
	for _, name := range names {
		items = append(items, packageRemovalItem{
			Name:        name,
			Description: state[name],
			Kind:        kind,
		})
	}
	return items
}

func (i packageRemovalInventory) Empty() bool {
	return len(i.Native) == 0 && len(i.AUR) == 0
}

func (r *Runner) selectPackagesForRemoval(inventory packageRemovalInventory) ([]packageRemovalItem, error) {
	if r.packageRemovalTUI != nil {
		return r.packageRemovalTUI(inventory)
	}
	if !interactiveTerminal(r.Stdin, r.Stdout) {
		return nil, fmt.Errorf("archstate packages requires an interactive terminal")
	}

	program := tea.NewProgram(
		newPackageRemovalModel(inventory),
		tea.WithInput(r.Stdin),
		tea.WithOutput(r.Stdout),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	finalModel, err := program.Run()
	if err != nil {
		return nil, err
	}
	model, ok := finalModel.(packageRemovalModel)
	if !ok || !model.confirmed {
		return nil, nil
	}
	return model.selectedItems(), nil
}

func interactiveTerminal(in io.Reader, out io.Writer) bool {
	inFile, ok := in.(*os.File)
	if !ok {
		return false
	}
	outFile, ok := out.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(inFile.Fd()) && isatty.IsTerminal(outFile.Fd())
}

func (r *Runner) removePackages(items []packageRemovalItem) error {
	args := packageRemovalCommandArgs(items)
	if len(args) == 0 {
		return nil
	}
	return r.streamCommand("sudo", args...)
}

func packageRemovalCommandArgs(items []packageRemovalItem) []string {
	names := packageRemovalCommandNames(items)
	if len(names) == 0 {
		return nil
	}
	args := append([]string{"pacman", "-Rns"}, names...)
	return args
}

func packageRemovalCommandNames(items []packageRemovalItem) []string {
	seen := make(map[string]bool, len(items))
	names := make([]string, 0, len(items))
	for _, item := range items {
		if item.Name == "" || seen[item.Name] {
			continue
		}
		seen[item.Name] = true
		names = append(names, item.Name)
	}
	sort.Strings(names)
	return names
}

type packageRemovalSection int

const (
	packageRemovalSectionNative packageRemovalSection = iota
	packageRemovalSectionAUR
)

type packageRemovalFocus int

const (
	packageRemovalFocusSearch packageRemovalFocus = iota
	packageRemovalFocusPackages
	packageRemovalFocusMarked
)

type packageRemovalModel struct {
	inventory packageRemovalInventory
	active    packageRemovalSection
	focus     packageRemovalFocus
	query     string

	packageCursor [2]int
	packageOffset [2]int
	markedCursor  int
	marked        map[string]packageRemovalItem
	lastClick     packageRemovalClick

	confirming    bool
	confirmCursor int
	confirmOffset int
	confirmed     bool
	width         int
	height        int
}

type packageRemovalClick struct {
	Section packageRemovalSection
	Index   int
	Name    string
	Valid   bool
}

func newPackageRemovalModel(inventory packageRemovalInventory) packageRemovalModel {
	return packageRemovalModel{
		inventory: inventory,
		focus:     packageRemovalFocusSearch,
		marked:    make(map[string]packageRemovalItem),
		width:     100,
		height:    30,
	}
}

func (m packageRemovalModel) Init() tea.Cmd {
	return nil
}

func (m packageRemovalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampCursors()
		m.clampConfirmCursor()
		m.lastClick.Valid = false
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	case tea.MouseMsg:
		return m.updateMouse(msg)
	default:
		return m, nil
	}
}

func (m packageRemovalModel) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.confirming {
		return m, nil
	}
	if msg.Action != tea.MouseActionPress {
		if msg.Action == tea.MouseActionMotion {
			m.lastClick.Valid = false
		}
		return m, nil
	}
	if msg.Y == 0 && msg.Button == tea.MouseButtonLeft {
		m.focus = packageRemovalFocusSearch
		m.lastClick.Valid = false
		return m, nil
	}

	leftWidth, rightWidth, panelHeight := m.layout()
	rightPaneX := leftWidth + 1
	if msg.X < rightPaneX || msg.X >= rightPaneX+rightWidth || msg.Y < 1 || msg.Y >= 1+panelHeight {
		m.lastClick.Valid = false
		return m, nil
	}

	wasPackageFocus := m.focus == packageRemovalFocusPackages
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.focus = packageRemovalFocusPackages
		m.scrollPackageList(-3)
		m.lastClick.Valid = false
		return m, nil
	case tea.MouseButtonWheelDown:
		m.focus = packageRemovalFocusPackages
		m.scrollPackageList(3)
		m.lastClick.Valid = false
		return m, nil
	case tea.MouseButtonLeft:
		m.focus = packageRemovalFocusPackages
	default:
		m.lastClick.Valid = false
		return m, nil
	}

	contentX := rightPaneX + 2
	contentY := 2
	if msg.X < contentX || msg.X >= contentX+rightWidth-4 || msg.Y < contentY {
		m.lastClick.Valid = false
		return m, nil
	}
	if msg.Y == contentY {
		if section, ok := m.packageTabHit(msg.X - contentX); ok {
			m.active = section
			m.clampCursors()
		}
		m.lastClick.Valid = false
		return m, nil
	}

	headerLines := len(m.packageTabHeaderLines(rightWidth - 4))
	listY := contentY + headerLines
	bodyHeight := panelHeight - 2 - headerLines
	row := msg.Y - listY
	if row < 0 || row >= bodyHeight {
		m.lastClick.Valid = false
		return m, nil
	}
	visible := m.visiblePackages()
	start := m.packageOffsetForView(m.active, len(visible), bodyHeight)
	idx := start + row
	if idx >= 0 && idx < len(visible) {
		item := visible[idx]
		click := packageRemovalClick{
			Section: m.active,
			Index:   idx,
			Name:    item.Name,
			Valid:   true,
		}
		if m.lastClick == click {
			m.packageCursor[int(m.active)] = idx
			m.togglePackageItem(item)
			m.lastClick.Valid = false
		} else {
			if !wasPackageFocus {
				m.packageCursor[int(m.active)] = idx
			}
			m.lastClick = click
		}
	} else {
		m.lastClick.Valid = false
	}
	m.clampCursors()
	return m, nil
}

func (m packageRemovalModel) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.lastClick.Valid = false
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.confirming {
		return m.updateConfirmKey(key)
	}
	if key == "1" {
		m.active = packageRemovalSectionNative
		m.clampCursors()
		return m, nil
	}
	if key == "2" {
		m.active = packageRemovalSectionAUR
		m.clampCursors()
		return m, nil
	}

	switch m.focus {
	case packageRemovalFocusSearch:
		return m.updateSearchKey(msg)
	case packageRemovalFocusPackages:
		return m.updatePackageListKey(key)
	case packageRemovalFocusMarked:
		return m.updateMarkedListKey(key)
	default:
		return m, nil
	}
}

func (m packageRemovalModel) updateConfirmKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "enter", "y":
		m.confirmed = true
		return m, tea.Quit
	case "esc", "n":
		m.confirming = false
		return m, nil
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.confirmCursor--
		m.clampConfirmCursor()
	case "down", "j":
		m.confirmCursor++
		m.clampConfirmCursor()
	case " ", "space":
		items := m.markedItemsGrouped()
		if len(items) > 0 {
			delete(m.marked, items[m.confirmCursor].Name)
			if len(m.marked) == 0 {
				// Nothing left to confirm; drop back to the package list.
				m.confirming = false
				m.confirmCursor = 0
				m.confirmOffset = 0
				return m, nil
			}
			m.clampConfirmCursor()
		}
	}
	return m, nil
}

func (m *packageRemovalModel) clampConfirmCursor() {
	total := len(m.marked)
	m.confirmCursor = clampIndex(m.confirmCursor, total)
	m.confirmOffset = packageRemovalOffsetForCursor(m.confirmCursor, m.confirmOffset, total, m.confirmBodyHeight())
}

// confirmPanelHeight is the outer height (incl. border) of the review panel.
// The review view is title (1) + panel + command (1) + footer (1), so this
// leaves one line of headroom under the terminal height.
func (m packageRemovalModel) confirmPanelHeight() int {
	return max(m.height-4, 4)
}

func (m packageRemovalModel) confirmBodyHeight() int {
	return max(m.confirmPanelHeight()-2, 1)
}

func (m packageRemovalModel) updateSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "enter", "down":
		m.focus = packageRemovalFocusPackages
	case "tab":
		m.focus = packageRemovalFocusPackages
	case "backspace", "ctrl+h":
		m.query = trimLastRune(m.query)
	case "esc":
		return m, tea.Quit
	default:
		if len(msg.Runes) > 0 && !msg.Alt {
			m.query += string(msg.Runes)
		}
	}
	m.clampCursors()
	return m, nil
}

func (m packageRemovalModel) updatePackageListKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "tab", "shift+tab":
		if len(m.marked) > 0 {
			m.focus = packageRemovalFocusMarked
		}
	case "f", "/":
		m.focus = packageRemovalFocusSearch
	case "q":
		return m, tea.Quit
	case "esc":
		return m, tea.Quit
	case "up", "k":
		m.movePackageCursor(-1)
	case "down", "j":
		m.movePackageCursor(1)
	case " ", "space":
		m.toggleFocusedPackage()
	case "enter":
		if len(m.marked) > 0 {
			m.confirming = true
			m.confirmCursor = 0
			m.confirmOffset = 0
		}
	}
	m.clampCursors()
	return m, nil
}

func (m packageRemovalModel) updateMarkedListKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "tab", "shift+tab":
		m.focus = packageRemovalFocusPackages
	case "f", "/":
		m.focus = packageRemovalFocusSearch
	case "q":
		return m, tea.Quit
	case "esc":
		return m, tea.Quit
	case "up", "k":
		m.markedCursor--
	case "down", "j":
		m.markedCursor++
	case " ", "space":
		items := m.markedItemsGrouped()
		if len(items) > 0 {
			delete(m.marked, items[m.markedCursor].Name)
		}
	case "enter":
		if len(m.marked) > 0 {
			m.confirming = true
			m.confirmCursor = 0
			m.confirmOffset = 0
		}
	}
	m.clampCursors()
	return m, nil
}

func (m *packageRemovalModel) movePackageCursor(delta int) {
	idx := int(m.active)
	m.packageCursor[idx] += delta
	// clampCursors already recomputes the active section's offset from the
	// (now-updated) cursor via packageRemovalOffsetForCursor, so an explicit
	// ensurePackageCursorVisible here would be an idempotent no-op.
	m.clampCursors()
}

func (m *packageRemovalModel) scrollPackageList(delta int) {
	idx := int(m.active)
	visible := m.visiblePackages()
	bodyHeight := m.packageListBodyHeight()
	m.packageOffset[idx] = clampPackageOffset(m.packageOffset[idx]+delta, len(visible), bodyHeight)
	if len(visible) == 0 {
		m.packageCursor[idx] = 0
		return
	}
	if m.packageCursor[idx] < m.packageOffset[idx] {
		m.packageCursor[idx] = m.packageOffset[idx]
	}
	maxVisibleCursor := m.packageOffset[idx] + bodyHeight - 1
	if maxVisibleCursor >= len(visible) {
		maxVisibleCursor = len(visible) - 1
	}
	if m.packageCursor[idx] > maxVisibleCursor {
		m.packageCursor[idx] = maxVisibleCursor
	}
}

func (m *packageRemovalModel) toggleFocusedPackage() {
	items := m.visiblePackages()
	if len(items) == 0 {
		return
	}
	m.togglePackageItem(items[m.packageCursor[int(m.active)]])
}

func (m *packageRemovalModel) togglePackageItem(item packageRemovalItem) {
	if _, ok := m.marked[item.Name]; ok {
		delete(m.marked, item.Name)
		return
	}
	m.marked[item.Name] = item
}

func (m *packageRemovalModel) clampCursors() {
	for section := range m.packageCursor {
		items := m.visiblePackagesForSection(packageRemovalSection(section))
		m.packageCursor[section] = clampIndex(m.packageCursor[section], len(items))
		m.packageOffset[section] = packageRemovalOffsetForCursor(m.packageCursor[section], m.packageOffset[section], len(items), m.packageListBodyHeight())
	}
	m.markedCursor = clampIndex(m.markedCursor, len(m.marked))
}

func (m *packageRemovalModel) ensurePackageCursorVisible(section packageRemovalSection) {
	idx := int(section)
	total := len(m.visiblePackagesForSection(section))
	bodyHeight := m.packageListBodyHeight()
	m.packageOffset[idx] = packageRemovalOffsetForCursor(m.packageCursor[idx], m.packageOffset[idx], total, bodyHeight)
}

func (m packageRemovalModel) packageOffsetForView(section packageRemovalSection, total, bodyHeight int) int {
	idx := int(section)
	return packageRemovalOffsetForCursor(m.packageCursor[idx], m.packageOffset[idx], total, bodyHeight)
}

func (m packageRemovalModel) packageListBodyHeight() int {
	_, rightWidth, panelHeight := m.layout()
	return panelHeight - 2 - len(m.packageTabHeaderLines(rightWidth-4))
}

func clampIndex(idx, length int) int {
	if length <= 0 {
		return 0
	}
	return min(max(idx, 0), length-1)
}

func clampPackageOffset(offset, total, height int) int {
	if total <= 0 || height <= 0 || total <= height {
		return 0
	}
	maxOffset := total - height
	if offset < 0 {
		return 0
	}
	if offset > maxOffset {
		return maxOffset
	}
	return offset
}

func packageRemovalOffsetForCursor(cursor, offset, total, height int) int {
	offset = clampPackageOffset(offset, total, height)
	if total <= 0 || height <= 0 {
		return 0
	}
	cursor = clampIndex(cursor, total)
	if cursor < offset {
		return cursor
	}
	if cursor >= offset+height {
		return clampPackageOffset(cursor-height+1, total, height)
	}
	return offset
}

func (m packageRemovalModel) visiblePackages() []packageRemovalItem {
	return m.visiblePackagesForSection(m.active)
}

func (m packageRemovalModel) visiblePackagesForSection(section packageRemovalSection) []packageRemovalItem {
	items := m.inventory.itemsForSection(section)
	query := strings.TrimSpace(m.query)
	if query == "" {
		return append([]packageRemovalItem{}, items...)
	}

	type scoredItem struct {
		item  packageRemovalItem
		score int
	}
	matches := make([]scoredItem, 0, len(items))
	for _, item := range items {
		if score, ok := fuzzyPackageScore(item.Name, query); ok {
			matches = append(matches, scoredItem{item: item, score: score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].item.Name < matches[j].item.Name
	})
	filtered := make([]packageRemovalItem, 0, len(matches))
	for _, match := range matches {
		filtered = append(filtered, match.item)
	}
	return filtered
}

func fuzzyPackageScore(candidate, query string) (int, bool) {
	indexes, ok := fuzzyPackageMatchIndexes(candidate, query)
	if !ok {
		return 0, false
	}
	candidateRunes := []rune(strings.ToLower(candidate))
	queryRunes := []rune(strings.ToLower(query))
	if len(queryRunes) == 0 {
		return 0, true
	}

	firstMatch := indexes[0]
	lastMatch := -2
	contiguous := 0
	gaps := 0
	for _, idx := range indexes {
		if idx == lastMatch+1 {
			contiguous++
		} else if lastMatch >= 0 {
			gaps += idx - lastMatch - 1
		}
		lastMatch = idx
	}

	score := 1000
	score -= firstMatch * 10
	score -= gaps * 3
	score += contiguous * 20
	score -= len(candidateRunes)
	if strings.HasPrefix(strings.ToLower(candidate), strings.ToLower(query)) {
		score += 200
	}
	return score, true
}

func fuzzyPackageMatchIndexes(candidate, query string) ([]int, bool) {
	candidateRunes := []rune(strings.ToLower(candidate))
	queryRunes := []rune(strings.ToLower(query))
	if len(queryRunes) == 0 {
		return nil, true
	}

	queryIndex := 0
	indexes := make([]int, 0, len(queryRunes))
	for i, r := range candidateRunes {
		if queryIndex >= len(queryRunes) {
			break
		}
		if r != queryRunes[queryIndex] {
			continue
		}
		indexes = append(indexes, i)
		queryIndex++
	}
	if queryIndex != len(queryRunes) {
		return nil, false
	}
	return indexes, true
}

func (i packageRemovalInventory) itemsForSection(section packageRemovalSection) []packageRemovalItem {
	if section == packageRemovalSectionAUR {
		return i.AUR
	}
	return i.Native
}

func (m packageRemovalModel) selectedItems() []packageRemovalItem {
	items := make([]packageRemovalItem, 0, len(m.marked))
	for _, item := range m.marked {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items
}

func (m packageRemovalModel) markedItemsGrouped() []packageRemovalItem {
	items := make([]packageRemovalItem, 0, len(m.marked))
	for _, kind := range []packageRemovalKind{packageRemovalNative, packageRemovalAUR} {
		group := make([]packageRemovalItem, 0)
		for _, item := range m.marked {
			if item.Kind == kind {
				group = append(group, item)
			}
		}
		sort.Slice(group, func(i, j int) bool {
			return group[i].Name < group[j].Name
		})
		items = append(items, group...)
	}
	return items
}

func (m packageRemovalModel) View() string {
	if m.confirming {
		return m.confirmationView()
	}

	leftWidth, rightWidth, panelHeight := m.layout()
	search := m.searchView(leftWidth + rightWidth + 1)
	left := m.markedPaneView(leftWidth, panelHeight)
	right := m.packagePaneView(rightWidth, panelHeight)
	footer := packageRemovalFooterStyle.Render(centerText("1/2= section | tab= panes | f,/= search | space= mark | enter= confirm | esc= exit", leftWidth+rightWidth+1))

	return strings.Join([]string{
		search,
		lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right),
		footer,
	}, "\n")
}

func (m packageRemovalModel) packageTabHeaderLines(width int) []string {
	if width < 1 {
		width = 1
	}
	nativeTab := packageRemovalTab(1, "Native", len(m.inventory.Native), m.active == packageRemovalSectionNative)
	aurTab := packageRemovalTab(2, "AUR", len(m.inventory.AUR), m.active == packageRemovalSectionAUR)
	return []string{
		fitText(nativeTab+" "+aurTab, width),
		strings.Repeat("-", width),
	}
}

func packageRemovalTab(number int, label string, count int, active bool) string {
	text := packageRemovalTabText(number, label, count, active)
	if active {
		return packageRemovalActiveTabStyle.Render(text)
	}
	return packageRemovalInactiveTabStyle.Render(text)
}

func packageRemovalTabText(number int, label string, count int, active bool) string {
	text := fmt.Sprintf("(%d) %s %d", number, label, count)
	if active {
		return "[" + text + "]"
	}
	return text
}

func (m packageRemovalModel) packageTabHit(x int) (packageRemovalSection, bool) {
	native := packageRemovalTabText(1, "Native", len(m.inventory.Native), m.active == packageRemovalSectionNative)
	aur := packageRemovalTabText(2, "AUR", len(m.inventory.AUR), m.active == packageRemovalSectionAUR)
	if x >= 0 && x < lipgloss.Width(native) {
		return packageRemovalSectionNative, true
	}
	aurStart := lipgloss.Width(native) + 1
	if x >= aurStart && x < aurStart+lipgloss.Width(aur) {
		return packageRemovalSectionAUR, true
	}
	return m.active, false
}

func (m packageRemovalModel) searchView(width int) string {
	prefix := "  "
	if m.focus == packageRemovalFocusSearch {
		prefix = ""
	}
	query := m.query
	if query == "" {
		query = "type to fuzzy-search"
		if m.focus != packageRemovalFocusSearch {
			query = packageRemovalMutedStyle.Render(query)
		}
	}
	line := prefix + "Search: " + query
	line = centerText(line, width)
	if m.focus == packageRemovalFocusSearch {
		return packageRemovalSearchFocusStyle.Render(padText(line, width))
	}
	return line
}

func (m packageRemovalModel) markedPaneView(width, height int) string {
	contentWidth := width - 4
	lines := []string{
		fitText(fmt.Sprintf("Marked (%d)", len(m.marked)), contentWidth),
		strings.Repeat("-", contentWidth),
	}
	items := m.markedItemsGrouped()
	if len(items) == 0 {
		lines = append(lines, packageRemovalMutedStyle.Render("none"))
	} else {
		lastKind := packageRemovalKind(-1)
		for idx, item := range items {
			if item.Kind != lastKind {
				lines = append(lines, packageRemovalMutedStyle.Render(packageRemovalKindLabel(item.Kind)))
				lastKind = item.Kind
			}
			cursor := " "
			if m.focus == packageRemovalFocusMarked && idx == m.markedCursor {
				cursor = ">"
			}
			line := fmt.Sprintf("%s [x] %s", cursor, item.Name)
			lines = append(lines, packageRemovalRow(line, contentWidth, m.focus == packageRemovalFocusMarked && idx == m.markedCursor))
		}
	}
	return packageRemovalPanelStyle(width, height).Render(strings.Join(fitLines(lines, height-2), "\n"))
}

func (m packageRemovalModel) packagePaneView(width, height int) string {
	contentWidth := width - 4
	visible := m.visiblePackages()
	lines := m.packageTabHeaderLines(contentWidth)
	if len(visible) == 0 {
		lines = append(lines, packageRemovalMutedStyle.Render("no matches"))
	} else {
		bodyHeight := height - 2 - len(lines)
		start := m.packageOffsetForView(m.active, len(visible), bodyHeight)
		end := min(start+bodyHeight, len(visible))
		for idx := start; idx < end; idx++ {
			item := visible[idx]
			cursor := " "
			if m.focus == packageRemovalFocusPackages && idx == m.packageCursor[int(m.active)] {
				cursor = ">"
			}
			mark := " "
			if _, ok := m.marked[item.Name]; ok {
				mark = "x"
			}
			line := fmt.Sprintf("%s [%s] %s", cursor, mark, m.highlightPackageName(item.Name))
			if item.Description != "" {
				line += " - " + item.Description
			}
			lines = append(lines, packageRemovalRow(line, contentWidth, m.focus == packageRemovalFocusPackages && idx == m.packageCursor[int(m.active)]))
		}
	}
	return packageRemovalPanelStyle(width, height).Render(strings.Join(fitLines(lines, height-2), "\n"))
}

func (m packageRemovalModel) highlightPackageName(name string) string {
	query := strings.TrimSpace(m.query)
	indexes, ok := fuzzyPackageMatchIndexes(name, query)
	if !ok || len(indexes) == 0 {
		return name
	}
	matched := make(map[int]bool, len(indexes))
	for _, idx := range indexes {
		matched[idx] = true
	}
	var out strings.Builder
	for idx, r := range []rune(name) {
		part := string(r)
		if matched[idx] {
			out.WriteString(packageRemovalMatchStyle.Render(part))
		} else {
			out.WriteString(part)
		}
	}
	return out.String()
}

func (m packageRemovalModel) confirmationView() string {
	leftWidth, rightWidth, _ := m.layout()
	width := leftWidth + rightWidth + 1
	contentWidth := width - 4
	panelHeight := m.confirmPanelHeight()
	bodyHeight := max(panelHeight-2, 1)

	items := m.markedItemsGrouped()
	nativeCount := 0
	for _, item := range items {
		if item.Kind == packageRemovalNative {
			nativeCount++
		}
	}
	title := centerText(packageRemovalTitleStyle.Render(
		fmt.Sprintf("Review removal — %d package(s): %d native, %d AUR", len(items), nativeCount, len(items)-nativeCount)), width)

	offset := packageRemovalOffsetForCursor(m.confirmCursor, m.confirmOffset, len(items), bodyHeight)
	end := min(offset+bodyHeight, len(items))
	bodyLines := make([]string, 0, bodyHeight)
	for idx := offset; idx < end; idx++ {
		item := items[idx]
		marker := " "
		if idx == m.confirmCursor {
			marker = ">"
		}
		label := item.Name
		if item.Kind == packageRemovalAUR {
			label += packageRemovalMutedStyle.Render(" (AUR)")
		}
		row := fmt.Sprintf("%s [x] %s", marker, label)
		if item.Description != "" {
			row += " - " + item.Description
		}
		bodyLines = append(bodyLines, packageRemovalRow(row, contentWidth, idx == m.confirmCursor))
	}
	panel := packageRemovalPanelStyle(width, panelHeight).Render(strings.Join(fitLines(bodyLines, bodyHeight), "\n"))

	command := fitText("Command: sudo "+strings.Join(packageRemovalCommandArgs(items), " "), width)
	footer := packageRemovalFooterStyle.Render(centerText("space= unmark | up/down= move | enter/y= remove | esc/n= back | q= quit", width))

	return strings.Join([]string{title, panel, command, footer}, "\n")
}

func (m packageRemovalModel) layout() (leftWidth, rightWidth, panelHeight int) {
	width := max(m.width, 80) - 1
	leftWidth = min(max(width/3, 24), 40)
	rightWidth = max(width-leftWidth-1, 40)
	panelHeight = max(m.height-4, 8)
	return leftWidth, rightWidth, panelHeight
}

func packageRemovalPanelStyle(width, height int) lipgloss.Style {
	// width/height are the full outer pane size. lipgloss Width counts content +
	// padding (the border is added outside it), so the inner text area is
	// Width - 2*horizontalPadding. The panes format their rows to width-4, so the
	// style Width must be width-2 to leave exactly that much text room; otherwise
	// every row is 2 columns too wide, wraps to a second line, and the pane grows
	// past its height (which pushes the search bar off the top of the screen).
	innerWidth := max(width-2, 1)
	contentHeight := max(height-2, 1)
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(innerWidth).
		Height(contentHeight).
		Padding(0, 1)
}

func packageRemovalKindLabel(kind packageRemovalKind) string {
	if kind == packageRemovalAUR {
		return "AUR"
	}
	return "Native"
}

func fitLines(lines []string, max int) []string {
	if max <= 0 || len(lines) <= max {
		return lines
	}
	out := append([]string{}, lines[:max]...)
	out[max-1] = packageRemovalMutedStyle.Render("...")
	return out
}

func fitText(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= width {
		return text
	}
	if width <= 3 {
		return ansi.Truncate(text, width, "")
	}
	return ansi.Truncate(text, width, "...")
}

func packageRemovalRow(text string, width int, highlight bool) string {
	text = padText(fitText(text, width), width)
	if !highlight {
		return text
	}
	return packageRemovalCursorLineStyle.Render(text)
}

func padText(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if got := lipgloss.Width(text); got < width {
		return text + strings.Repeat(" ", width-got)
	}
	return text
}

func centerText(text string, width int) string {
	if width <= 0 {
		return ""
	}
	got := lipgloss.Width(text)
	if got >= width {
		return text
	}
	left := (width - got) / 2
	right := width - got - left
	return strings.Repeat(" ", left) + text + strings.Repeat(" ", right)
}

func trimLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[:len(runes)-1])
}

var (
	packageRemovalTitleStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("15"))
	packageRemovalActiveTabStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("12"))
	packageRemovalInactiveTabStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("8"))
	packageRemovalMutedStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("8"))
	packageRemovalCursorLineStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("15")).
					Background(lipgloss.Color("238"))
	packageRemovalSearchFocusStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("15")).
					Background(lipgloss.Color("238"))
	packageRemovalMatchStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("11"))
	packageRemovalFooterStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("14"))
)
