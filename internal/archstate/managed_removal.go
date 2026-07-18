package archstate

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type managedRemovalKind int

const (
	managedRemovalConfig managedRemovalKind = iota
	managedRemovalHome
)

type managedRemovalItem struct {
	Name   string
	Status string
	Kind   managedRemovalKind
}

func (item managedRemovalItem) markKey() string {
	return fmt.Sprintf("%d:%s", item.Kind, item.Name)
}

type managedRemovalInventory struct {
	Config []managedRemovalItem
	Home   []managedRemovalItem
}

func (i managedRemovalInventory) Empty() bool {
	return len(i.Config) == 0 && len(i.Home) == 0
}

func (i managedRemovalInventory) itemsForSection(section managedRemovalSection) []managedRemovalItem {
	if section == managedRemovalSectionHome {
		return i.Home
	}
	return i.Config
}

type managedRemovalTUIFunc func(managedRemovalInventory) ([]managedRemovalItem, error)

func loadManagedRemovalInventory(repo repoPaths) (managedRemovalInventory, error) {
	configState, err := readStateFileStrictOptional(repo.configPath(), validateManagedEntry)
	if err != nil {
		return managedRemovalInventory{}, err
	}
	homeState, err := readStateFileStrictOptional(repo.homePath(), validateManagedEntry)
	if err != nil {
		return managedRemovalInventory{}, err
	}
	return managedRemovalInventory{
		Config: managedRemovalItemsFromState(configRoot(repo), configState, managedRemovalConfig),
		Home:   managedRemovalItemsFromState(homeRoot(repo), homeState, managedRemovalHome),
	}, nil
}

func managedRemovalItemsFromState(root managedRoot, state map[string]string, kind managedRemovalKind) []managedRemovalItem {
	names := sortedEntryKeys(state)
	items := make([]managedRemovalItem, 0, len(names))
	for _, name := range names {
		action := planManagedEntry(root, name, state[name], BootstrapOptions{})
		items = append(items, managedRemovalItem{
			Name:   name,
			Status: managedRemovalStatus(action),
			Kind:   kind,
		})
	}
	return items
}

func managedRemovalStatus(action ManagedAction) string {
	switch action.Kind {
	case ManagedNoopAction:
		return "ok"
	case ManagedSymlinkAction:
		return "missing"
	case ManagedConflictAction:
		return "conflict"
	case ManagedErrorAction:
		return "error"
	default:
		return string(action.Kind)
	}
}

// runManagedAs opens the managed untrack TUI. cmd is the user-facing verb used in
// usage and terminal-required errors ("track" or "track untrack").
func (r *Runner) runManagedAs(cmd string, args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		return r.printCommandHelp("track")
	}
	if len(args) != 0 {
		return fmt.Errorf("usage: archstate %s", cmd)
	}

	repo, err := r.discoverExistingRepo()
	if err != nil {
		return err
	}
	if r.managedRemovalTUI == nil && !interactiveTerminal(r.Stdin, r.Stdout) {
		return fmt.Errorf("archstate %s requires an interactive terminal; run it directly in a terminal, not through a pipe or in a script", cmd)
	}

	return r.withRepoLock(repo, cmd, func() error {
		inventory, err := loadManagedRemovalInventory(repo)
		if err != nil {
			return err
		}
		if inventory.Empty() {
			fmt.Fprintln(r.Stdout, "no managed config or home entries to untrack")
			return nil
		}

		selected, err := r.selectManagedForUntrack(inventory)
		if err != nil {
			return err
		}
		if len(selected) == 0 {
			fmt.Fprintln(r.Stdout, "no entries selected")
			return nil
		}

		var configNames, homeNames []string
		for _, item := range selected {
			switch item.Kind {
			case managedRemovalConfig:
				configNames = append(configNames, item.Name)
			case managedRemovalHome:
				homeNames = append(homeNames, item.Name)
			}
		}
		// Already holding the repo lock from withRepoLock above — call locked path.
		batches := make([]managedUntrackBatch, 0, 2)
		if len(configNames) > 0 {
			batches = append(batches, managedUntrackBatch{
				root:       configRoot(repo),
				configPath: repo.configPath(),
				names:      configNames,
			})
		}
		if len(homeNames) > 0 {
			batches = append(batches, managedUntrackBatch{
				root:       homeRoot(repo),
				configPath: repo.homePath(),
				names:      homeNames,
			})
		}
		if err := r.untrackManagedRootsLocked(repo, batches); err != nil {
			return err
		}
		fmt.Fprintf(r.Stdout, "stopped managing %d entries\n", len(selected))
		return nil
	})
}

func (r *Runner) selectManagedForUntrack(inventory managedRemovalInventory) ([]managedRemovalItem, error) {
	if r.managedRemovalTUI != nil {
		return r.managedRemovalTUI(inventory)
	}
	if !interactiveTerminal(r.Stdin, r.Stdout) {
		return nil, fmt.Errorf("archstate track requires an interactive terminal; run it directly in a terminal, not through a pipe or in a script")
	}

	program := tea.NewProgram(
		newManagedRemovalModel(inventory),
		tea.WithInput(r.Stdin),
		tea.WithOutput(r.Stdout),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	finalModel, err := program.Run()
	if err != nil {
		return nil, err
	}
	model, ok := finalModel.(managedRemovalModel)
	if !ok || !model.confirmed {
		return nil, nil
	}
	return model.selectedItems(), nil
}

type managedRemovalSection int

const (
	managedRemovalSectionConfig managedRemovalSection = iota
	managedRemovalSectionHome
)

type managedRemovalFocus int

const (
	managedRemovalFocusSearch managedRemovalFocus = iota
	managedRemovalFocusList
	managedRemovalFocusMarked
)

type managedRemovalModel struct {
	inventory managedRemovalInventory
	active    managedRemovalSection
	focus     managedRemovalFocus
	query     string

	listCursor   [2]int
	listOffset   [2]int
	markedCursor int
	marked       map[string]managedRemovalItem
	lastClick    managedRemovalClick

	confirming    bool
	confirmCursor int
	confirmOffset int
	confirmed     bool
	width         int
	height        int
}

type managedRemovalClick struct {
	Section managedRemovalSection
	Index   int
	Key     string
	Valid   bool
}

func newManagedRemovalModel(inventory managedRemovalInventory) managedRemovalModel {
	return managedRemovalModel{
		inventory: inventory,
		focus:     managedRemovalFocusSearch,
		marked:    make(map[string]managedRemovalItem),
		width:     100,
		height:    30,
	}
}

func (m managedRemovalModel) Init() tea.Cmd { return nil }

func (m managedRemovalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m managedRemovalModel) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
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
		m.focus = managedRemovalFocusSearch
		m.lastClick.Valid = false
		return m, nil
	}

	leftWidth, rightWidth, panelHeight := m.layout()
	rightPaneX := leftWidth + 1
	if msg.X < rightPaneX || msg.X >= rightPaneX+rightWidth || msg.Y < 1 || msg.Y >= 1+panelHeight {
		m.lastClick.Valid = false
		return m, nil
	}

	wasListFocus := m.focus == managedRemovalFocusList
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.focus = managedRemovalFocusList
		m.scrollList(-3)
		m.lastClick.Valid = false
		return m, nil
	case tea.MouseButtonWheelDown:
		m.focus = managedRemovalFocusList
		m.scrollList(3)
		m.lastClick.Valid = false
		return m, nil
	case tea.MouseButtonLeft:
		m.focus = managedRemovalFocusList
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
		if section, ok := m.tabHit(msg.X - contentX); ok {
			m.active = section
			m.clampCursors()
		}
		m.lastClick.Valid = false
		return m, nil
	}

	headerLines := len(m.tabHeaderLines(rightWidth - 4))
	listY := contentY + headerLines
	bodyHeight := panelHeight - 2 - headerLines
	row := msg.Y - listY
	if row < 0 || row >= bodyHeight {
		m.lastClick.Valid = false
		return m, nil
	}
	visible := m.visibleItems()
	start := m.listOffsetForView(m.active, len(visible), bodyHeight)
	idx := start + row
	if idx >= 0 && idx < len(visible) {
		item := visible[idx]
		click := managedRemovalClick{
			Section: m.active,
			Index:   idx,
			Key:     item.markKey(),
			Valid:   true,
		}
		if m.lastClick == click {
			m.listCursor[int(m.active)] = idx
			m.toggleItem(item)
			m.lastClick.Valid = false
		} else {
			if !wasListFocus {
				m.listCursor[int(m.active)] = idx
			}
			m.lastClick = click
		}
	} else {
		m.lastClick.Valid = false
	}
	m.clampCursors()
	return m, nil
}

func (m managedRemovalModel) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.lastClick.Valid = false
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.confirming {
		return m.updateConfirmKey(key)
	}
	if key == "1" {
		m.active = managedRemovalSectionConfig
		m.clampCursors()
		return m, nil
	}
	if key == "2" {
		m.active = managedRemovalSectionHome
		m.clampCursors()
		return m, nil
	}

	switch m.focus {
	case managedRemovalFocusSearch:
		return m.updateSearchKey(msg)
	case managedRemovalFocusList:
		return m.updateListKey(key)
	case managedRemovalFocusMarked:
		return m.updateMarkedKey(key)
	default:
		return m, nil
	}
}

func (m managedRemovalModel) updateConfirmKey(key string) (tea.Model, tea.Cmd) {
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
			delete(m.marked, items[m.confirmCursor].markKey())
			if len(m.marked) == 0 {
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

func (m *managedRemovalModel) clampConfirmCursor() {
	total := len(m.marked)
	m.confirmCursor = clampIndex(m.confirmCursor, total)
	m.confirmOffset = packageRemovalOffsetForCursor(m.confirmCursor, m.confirmOffset, total, m.confirmBodyHeight())
}

func (m managedRemovalModel) confirmPanelHeight() int {
	return max(m.height-5, 4)
}

func (m managedRemovalModel) confirmBodyHeight() int {
	return max(m.confirmPanelHeight()-2, 1)
}

func (m managedRemovalModel) updateSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "enter", "down", "tab":
		m.focus = managedRemovalFocusList
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

func (m managedRemovalModel) updateListKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "tab", "shift+tab":
		if len(m.marked) > 0 {
			m.focus = managedRemovalFocusMarked
		}
	case "f", "/":
		m.focus = managedRemovalFocusSearch
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		m.moveListCursor(-1)
	case "down", "j":
		m.moveListCursor(1)
	case " ", "space":
		m.toggleFocused()
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

func (m managedRemovalModel) updateMarkedKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "tab", "shift+tab":
		m.focus = managedRemovalFocusList
	case "f", "/":
		m.focus = managedRemovalFocusSearch
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		m.markedCursor--
	case "down", "j":
		m.markedCursor++
	case " ", "space":
		items := m.markedItemsGrouped()
		if len(items) > 0 {
			delete(m.marked, items[m.markedCursor].markKey())
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

func (m *managedRemovalModel) moveListCursor(delta int) {
	idx := int(m.active)
	m.listCursor[idx] += delta
	m.clampCursors()
}

func (m *managedRemovalModel) scrollList(delta int) {
	idx := int(m.active)
	visible := m.visibleItems()
	bodyHeight := m.listBodyHeight()
	m.listOffset[idx] = clampPackageOffset(m.listOffset[idx]+delta, len(visible), bodyHeight)
	if len(visible) == 0 {
		m.listCursor[idx] = 0
		return
	}
	if m.listCursor[idx] < m.listOffset[idx] {
		m.listCursor[idx] = m.listOffset[idx]
	}
	maxVisibleCursor := m.listOffset[idx] + bodyHeight - 1
	if maxVisibleCursor >= len(visible) {
		maxVisibleCursor = len(visible) - 1
	}
	if m.listCursor[idx] > maxVisibleCursor {
		m.listCursor[idx] = maxVisibleCursor
	}
}

func (m *managedRemovalModel) toggleFocused() {
	items := m.visibleItems()
	if len(items) == 0 {
		return
	}
	m.toggleItem(items[m.listCursor[int(m.active)]])
}

func (m *managedRemovalModel) toggleItem(item managedRemovalItem) {
	key := item.markKey()
	if _, ok := m.marked[key]; ok {
		delete(m.marked, key)
		return
	}
	m.marked[key] = item
}

func (m *managedRemovalModel) clampCursors() {
	for section := range m.listCursor {
		items := m.visibleItemsForSection(managedRemovalSection(section))
		m.listCursor[section] = clampIndex(m.listCursor[section], len(items))
		m.listOffset[section] = packageRemovalOffsetForCursor(m.listCursor[section], m.listOffset[section], len(items), m.listBodyHeight())
	}
	m.markedCursor = clampIndex(m.markedCursor, len(m.marked))
}

func (m managedRemovalModel) listOffsetForView(section managedRemovalSection, total, bodyHeight int) int {
	idx := int(section)
	return packageRemovalOffsetForCursor(m.listCursor[idx], m.listOffset[idx], total, bodyHeight)
}

func (m managedRemovalModel) listBodyHeight() int {
	_, rightWidth, panelHeight := m.layout()
	return panelHeight - 2 - len(m.tabHeaderLines(rightWidth-4))
}

func (m managedRemovalModel) visibleItems() []managedRemovalItem {
	return m.visibleItemsForSection(m.active)
}

func (m managedRemovalModel) visibleItemsForSection(section managedRemovalSection) []managedRemovalItem {
	items := m.inventory.itemsForSection(section)
	query := strings.TrimSpace(m.query)
	if query == "" {
		return append([]managedRemovalItem{}, items...)
	}
	type scored struct {
		item  managedRemovalItem
		score int
	}
	matches := make([]scored, 0, len(items))
	for _, item := range items {
		if score, ok := fuzzyPackageScore(item.Name, query); ok {
			matches = append(matches, scored{item: item, score: score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].item.Name < matches[j].item.Name
	})
	out := make([]managedRemovalItem, 0, len(matches))
	for _, match := range matches {
		out = append(out, match.item)
	}
	return out
}

func (m managedRemovalModel) selectedItems() []managedRemovalItem {
	return m.markedItemsGrouped()
}

func (m managedRemovalModel) markedItemsGrouped() []managedRemovalItem {
	items := make([]managedRemovalItem, 0, len(m.marked))
	for _, kind := range []managedRemovalKind{managedRemovalConfig, managedRemovalHome} {
		group := make([]managedRemovalItem, 0)
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

func (m managedRemovalModel) View() string {
	if m.confirming {
		return m.confirmationView()
	}

	leftWidth, rightWidth, panelHeight := m.layout()
	search := m.searchView(leftWidth + rightWidth + 1)
	left := m.markedPaneView(leftWidth, panelHeight)
	right := m.listPaneView(rightWidth, panelHeight)
	footer := packageRemovalFooterStyle.Render(centerText(
		"1/2= section | tab= panes | f,/= search | space= mark | enter= review | esc= exit",
		leftWidth+rightWidth+1,
	))

	return strings.Join([]string{
		search,
		lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right),
		footer,
	}, "\n")
}

func (m managedRemovalModel) tabHeaderLines(width int) []string {
	if width < 1 {
		width = 1
	}
	configTab := packageRemovalTab(1, "Config", len(m.inventory.Config), m.active == managedRemovalSectionConfig)
	homeTab := packageRemovalTab(2, "Home", len(m.inventory.Home), m.active == managedRemovalSectionHome)
	return []string{
		fitText(configTab+" "+homeTab, width),
		strings.Repeat("-", width),
	}
}

func (m managedRemovalModel) tabHit(x int) (managedRemovalSection, bool) {
	config := packageRemovalTabText(1, "Config", len(m.inventory.Config), m.active == managedRemovalSectionConfig)
	home := packageRemovalTabText(2, "Home", len(m.inventory.Home), m.active == managedRemovalSectionHome)
	if x >= 0 && x < lipgloss.Width(config) {
		return managedRemovalSectionConfig, true
	}
	homeStart := lipgloss.Width(config) + 1
	if x >= homeStart && x < homeStart+lipgloss.Width(home) {
		return managedRemovalSectionHome, true
	}
	return m.active, false
}

func (m managedRemovalModel) searchView(width int) string {
	prefix := "  "
	if m.focus == managedRemovalFocusSearch {
		prefix = ""
	}
	query := m.query
	if query == "" {
		query = "type to fuzzy-search"
		if m.focus != managedRemovalFocusSearch {
			query = packageRemovalMutedStyle.Render(query)
		}
	}
	line := prefix + "Search: " + query
	line = centerText(line, width)
	if m.focus == managedRemovalFocusSearch {
		return packageRemovalSearchFocusStyle.Render(padText(line, width))
	}
	return line
}

func (m managedRemovalModel) markedPaneView(width, height int) string {
	contentWidth := width - 4
	lines := []string{
		fitText(fmt.Sprintf("Marked (%d)", len(m.marked)), contentWidth),
		strings.Repeat("-", contentWidth),
	}
	items := m.markedItemsGrouped()
	if len(items) == 0 {
		lines = append(lines, packageRemovalMutedStyle.Render("none"))
	} else {
		lastKind := managedRemovalKind(-1)
		for idx, item := range items {
			if item.Kind != lastKind {
				lines = append(lines, packageRemovalMutedStyle.Render(managedRemovalKindLabel(item.Kind)))
				lastKind = item.Kind
			}
			cursor := " "
			if m.focus == managedRemovalFocusMarked && idx == m.markedCursor {
				cursor = ">"
			}
			line := fmt.Sprintf("%s [x] %s", cursor, item.Name)
			lines = append(lines, packageRemovalRow(line, contentWidth, m.focus == managedRemovalFocusMarked && idx == m.markedCursor))
		}
	}
	return packageRemovalPanelStyle(width, height).Render(strings.Join(fitLines(lines, height-2), "\n"))
}

func (m managedRemovalModel) listPaneView(width, height int) string {
	contentWidth := width - 4
	visible := m.visibleItems()
	lines := m.tabHeaderLines(contentWidth)
	if len(visible) == 0 {
		lines = append(lines, packageRemovalMutedStyle.Render("no matches"))
	} else {
		bodyHeight := height - 2 - len(lines)
		start := m.listOffsetForView(m.active, len(visible), bodyHeight)
		end := min(start+bodyHeight, len(visible))
		for idx := start; idx < end; idx++ {
			item := visible[idx]
			cursor := " "
			if m.focus == managedRemovalFocusList && idx == m.listCursor[int(m.active)] {
				cursor = ">"
			}
			mark := " "
			if _, ok := m.marked[item.markKey()]; ok {
				mark = "x"
			}
			line := fmt.Sprintf("%s [%s] %s", cursor, mark, m.highlightName(item.Name))
			if item.Status != "" {
				line += " - " + item.Status
			}
			lines = append(lines, packageRemovalRow(line, contentWidth, m.focus == managedRemovalFocusList && idx == m.listCursor[int(m.active)]))
		}
	}
	return packageRemovalPanelStyle(width, height).Render(strings.Join(fitLines(lines, height-2), "\n"))
}

func (m managedRemovalModel) highlightName(name string) string {
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

func (m managedRemovalModel) confirmationView() string {
	leftWidth, rightWidth, _ := m.layout()
	width := leftWidth + rightWidth + 1
	contentWidth := width - 4
	panelHeight := m.confirmPanelHeight()
	bodyHeight := max(panelHeight-2, 1)

	items := m.markedItemsGrouped()
	configCount := 0
	for _, item := range items {
		if item.Kind == managedRemovalConfig {
			configCount++
		}
	}
	title := centerText(packageRemovalTitleStyle.Render(
		fmt.Sprintf("Stop managing %d entries — %d config, %d home", len(items), configCount, len(items)-configCount)), width)
	subtitle := centerText(packageRemovalMutedStyle.Render(
		"Does not delete your files; restores local copies when linked."), width)

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
		if item.Kind == managedRemovalHome {
			label += packageRemovalMutedStyle.Render(" (home)")
		} else {
			label += packageRemovalMutedStyle.Render(" (config)")
		}
		row := fmt.Sprintf("%s [x] %s", marker, label)
		if item.Status != "" {
			row += " - " + item.Status
		}
		bodyLines = append(bodyLines, packageRemovalRow(row, contentWidth, idx == m.confirmCursor))
	}
	panel := packageRemovalPanelStyle(width, panelHeight).Render(strings.Join(fitLines(bodyLines, bodyHeight), "\n"))
	footer := packageRemovalFooterStyle.Render(centerText(
		"space= unmark | up/down= move | enter/y= stop managing | esc/n= back | q= quit", width))

	return strings.Join([]string{title, subtitle, panel, footer}, "\n")
}

func (m managedRemovalModel) layout() (leftWidth, rightWidth, panelHeight int) {
	width := max(m.width, 80) - 1
	leftWidth = min(max(width/3, 24), 40)
	rightWidth = max(width-leftWidth-1, 40)
	panelHeight = max(m.height-4, 8)
	return leftWidth, rightWidth, panelHeight
}

func managedRemovalKindLabel(kind managedRemovalKind) string {
	if kind == managedRemovalHome {
		return "Home"
	}
	return "Config"
}
