package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/nicobailon/treemux/internal/tui/theme"
)

type GridPanel struct {
	Name        string
	SessionName string
	Path        string
	Branch      string
	Content     string
	HasSession  bool
	IsOrphan    bool
	IsRecent    bool
	Modified    int
	Staged      int
	Windows     int
	Panes       int
	Processes   []string
}

type GridContentMsg struct {
	Contents map[string]string
}

type GridState struct {
	Index            int
	Cols             int
	Panels           []GridPanel
	Available        []GridPanel
	Filter           string
	Filtering        bool
	InAvailable      bool
	AvailIdx         int
	ScrollOffset     int
	DetailPanel      *GridPanel
	DetailIdx        int
	FilteredCache    []GridPanel
	AvailCache       []GridPanel
	FilterCacheKey   string
	SessionPanels    []GridPanel
	RecentPanels     []GridPanel
	CategoryCacheKey string
}

func (g *GridState) FilteredPanels() []GridPanel {
	if g.Filter == "" {
		return g.Panels
	}
	if g.FilterCacheKey == g.Filter && g.FilteredCache != nil {
		return g.FilteredCache
	}
	g.RebuildFilterCaches()
	return g.FilteredCache
}

func (g *GridState) FilteredAvailable() []GridPanel {
	if g.Filter == "" {
		return g.Available
	}
	if g.FilterCacheKey == g.Filter && g.AvailCache != nil {
		return g.AvailCache
	}
	g.RebuildFilterCaches()
	return g.AvailCache
}

func (g *GridState) RebuildFilterCaches() {
	filterLower := strings.ToLower(g.Filter)

	g.FilteredCache = make([]GridPanel, 0, len(g.Panels))
	for _, p := range g.Panels {
		if strings.Contains(strings.ToLower(p.Name), filterLower) ||
			strings.Contains(strings.ToLower(p.Branch), filterLower) ||
			strings.Contains(strings.ToLower(p.SessionName), filterLower) {
			g.FilteredCache = append(g.FilteredCache, p)
		}
	}

	g.AvailCache = make([]GridPanel, 0, len(g.Available))
	for _, p := range g.Available {
		if strings.Contains(strings.ToLower(p.Name), filterLower) ||
			strings.Contains(strings.ToLower(p.Branch), filterLower) {
			g.AvailCache = append(g.AvailCache, p)
		}
	}

	g.FilterCacheKey = g.Filter
}

func (g *GridState) InvalidateFilterCache() {
	g.FilterCacheKey = ""
	g.FilteredCache = nil
	g.AvailCache = nil
	g.CategoryCacheKey = ""
}

func (g *GridState) CategorizedPanels() (sessions, recents []GridPanel) {
	filtered := g.FilteredPanels()
	cacheKey := g.Filter + "|" + fmt.Sprintf("%d", len(filtered))
	if g.CategoryCacheKey == cacheKey {
		return g.SessionPanels, g.RecentPanels
	}
	g.SessionPanels = make([]GridPanel, 0)
	g.RecentPanels = make([]GridPanel, 0)
	for _, p := range filtered {
		if p.IsRecent {
			g.RecentPanels = append(g.RecentPanels, p)
		} else if p.HasSession || p.IsOrphan {
			g.SessionPanels = append(g.SessionPanels, p)
		}
	}
	g.CategoryCacheKey = cacheKey
	return g.SessionPanels, g.RecentPanels
}

func (g *GridState) UpdateScroll(width, height int) {
	gridWidth := width - 4
	if gridWidth < 32 {
		gridWidth = 32
	}
	minPanelWidth := 32
	g.Cols = gridWidth / minPanelWidth
	if g.Cols < 1 {
		g.Cols = 1
	}
	if g.Cols > 4 {
		g.Cols = 4
	}

	sessionPanels, recentPanels := g.CategorizedPanels()
	filteredLen := len(sessionPanels) + len(recentPanels)

	headerHeight := 3
	footerHeight := 3
	availableHeight := height - headerHeight - footerHeight
	if availableHeight < 1 {
		availableHeight = 1
	}

	panelLines := 6
	actionItemsLines := 5
	sectionHeaderLines := 3

	sessionsLines := 0
	if len(sessionPanels) > 0 {
		sessionsLines = sectionHeaderLines + ((len(sessionPanels)+g.Cols-1)/g.Cols)*panelLines
	}
	recentLines := 0
	if len(recentPanels) > 0 {
		recentLines = sectionHeaderLines + ((len(recentPanels)+g.Cols-1)/g.Cols)*panelLines
	}

	noMatchLines := 0
	if filteredLen == 0 && g.Filtering {
		noMatchLines = 1
	}

	var selectedLine int
	if g.InAvailable {
		availRowIdx := g.AvailIdx / g.Cols
		selectedLine = actionItemsLines + sessionsLines + recentLines + noMatchLines + sectionHeaderLines + availRowIdx*panelLines
	} else if g.Index < 0 {
		selectedLine = 0
	} else {
		if g.Index < len(sessionPanels) {
			rowIdx := g.Index / g.Cols
			selectedLine = actionItemsLines + sectionHeaderLines + rowIdx*panelLines
		} else if g.Index < len(sessionPanels)+len(recentPanels) {
			recentLocalIdx := g.Index - len(sessionPanels)
			rowIdx := recentLocalIdx / g.Cols
			selectedLine = actionItemsLines + sessionsLines + sectionHeaderLines + rowIdx*panelLines
		}
	}

	if selectedLine < g.ScrollOffset {
		g.ScrollOffset = selectedLine
	} else if selectedLine+panelLines > g.ScrollOffset+availableHeight {
		g.ScrollOffset = selectedLine + panelLines - availableHeight
	}
	if g.ScrollOffset < 0 {
		g.ScrollOffset = 0
	}
}

func (g *GridState) RenderView(width, height int) string {
	if len(g.Panels) == 0 && len(g.Available) == 0 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			theme.DimStyle.Render("No sessions or worktrees"))
	}

	gridWidth := width - 4
	if gridWidth < 32 {
		gridWidth = 32
	}

	minPanelWidth := 32
	g.Cols = gridWidth / minPanelWidth
	if g.Cols < 1 {
		g.Cols = 1
	}
	if g.Cols > 4 {
		g.Cols = 4
	}
	panelWidth := gridWidth / g.Cols
	innerWidth := panelWidth - 4

	title := theme.GridLogo

	if g.Filtering {
		filterStyle := lipgloss.NewStyle().
			Foreground(theme.BaseBg).
			Background(theme.Teal).
			Bold(true).
			Padding(0, 1)
		title += "  " + filterStyle.Render("/"+g.Filter+"_")
	}

	hint := theme.DimStyle.Render("/") + " " + theme.SubTextStyle.Render("filter") + "  " +
		theme.DimStyle.Render("1-9") + " " + theme.SubTextStyle.Render("quick jump") + "  " +
		theme.DimStyle.Render("enter") + " " + theme.SubTextStyle.Render("open") + "  " +
		theme.DimStyle.Render("ctrl+g") + " " + theme.SubTextStyle.Render("list view") + "  " +
		theme.DimStyle.Render("esc") + " " + theme.SubTextStyle.Render("back")

	header := lipgloss.NewStyle().Padding(1, 2).Render(title)

	filteredPanels := g.FilteredPanels()
	filteredAvailable := g.FilteredAvailable()

	if len(filteredPanels) == 0 && len(filteredAvailable) == 0 {
		noResults := lipgloss.NewStyle().Foreground(theme.DimColor).Render("No matching sessions")
		return lipgloss.JoinVertical(lipgloss.Left, header, "\n"+noResults)
	}

	var sessionPanels, recentPanels []GridPanel
	sessionIndices := make(map[int]int)
	recentIndices := make(map[int]int)
	for i, p := range filteredPanels {
		if p.IsRecent {
			recentIndices[len(recentPanels)] = i
			recentPanels = append(recentPanels, p)
		} else if p.HasSession || p.IsOrphan {
			sessionIndices[len(sessionPanels)] = i
			sessionPanels = append(sessionPanels, p)
		}
	}

	sectionHeaderStyle := theme.CachedDimStyle.MarginTop(1).MarginBottom(1)
	renderSectionHeader := func(text string) string {
		return sectionHeaderStyle.Render("── " + text + " " + strings.Repeat("─", gridWidth-len(text)-5))
	}

	renderPanel := func(panel GridPanel, globalIdx int, isSelected bool, isActive bool) string {
		borderColor := theme.SurfaceBg
		titleBg := theme.PanelBg
		if isSelected {
			borderColor = theme.SuccessColor
			titleBg = theme.SurfaceBg
		}

		var traffic string
		if isActive {
			traffic = theme.TrafficActive
		} else {
			traffic = theme.TrafficInactive
		}

		displayName := panel.Name
		maxNameLen := innerWidth - 8
		if maxNameLen < 5 {
			maxNameLen = 5
		}
		if len(displayName) > maxNameLen {
			displayName = displayName[:maxNameLen-1] + "…"
		}

		var nameStyle lipgloss.Style
		if isSelected {
			nameStyle = theme.CachedNameSelected
		} else if !isActive {
			nameStyle = theme.CachedNameMuted
		} else {
			nameStyle = theme.CachedNameStyle
		}

		titleContent := traffic + " " + nameStyle.Render(displayName)
		titleBar := lipgloss.NewStyle().
			Width(innerWidth).
			Background(titleBg).
			Padding(0, 1).
			Render(titleContent)

		line1 := ""
		if panel.IsOrphan {
			line1 = theme.CachedInactiveStyle.Render("* orphaned")
		} else if panel.Branch != "" {
			branchDisplay := panel.Branch
			maxBranchLen := innerWidth - 4
			if len(branchDisplay) > maxBranchLen {
				branchDisplay = branchDisplay[:maxBranchLen-1] + "…"
			}
			line1 = theme.CachedBranchStyle.Render("⎇ " + branchDisplay)
		}

		var line2 string
		if isActive {
			statusText := "● active"
			if panel.Windows > 0 {
				statusText += fmt.Sprintf(" %dw %dp", panel.Windows, panel.Panes)
			}
			line2 = theme.CachedActiveStyle.Render(statusText)
		} else {
			line2 = theme.CachedInactiveText
		}

		line3 := ""
		if globalIdx < 9 {
			line3 = theme.CachedInactiveStyle.Render(fmt.Sprintf("[%d]", globalIdx+1))
		}

		content := lipgloss.NewStyle().
			Width(innerWidth).
			Height(3).
			Padding(0, 1).
			Render(line1 + "\n" + line2 + "\n" + line3)

		panelContent := titleBar + "\n" + content

		return lipgloss.NewStyle().
			Width(panelWidth - 2).
			Border(theme.PanelBorder).
			BorderForeground(borderColor).
			Render(panelContent)
	}

	renderPanelGrid := func(panels []GridPanel, indices map[int]int, active bool) string {
		if len(panels) == 0 {
			return ""
		}
		var rows []string
		var currentRow []string
		for localIdx, panel := range panels {
			globalIdx := indices[localIdx]
			isSelected := globalIdx == g.Index && !g.InAvailable
			renderedPanel := renderPanel(panel, globalIdx, isSelected, active || panel.HasSession)
			currentRow = append(currentRow, renderedPanel)

			if len(currentRow) >= g.Cols || localIdx == len(panels)-1 {
				rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, currentRow...))
				currentRow = []string{}
			}
		}
		return lipgloss.JoinVertical(lipgloss.Left, rows...)
	}

	var gridSections []string

	renderActionItem := func(icon, actionTitle, desc string, selected bool) string {
		if selected {
			titleLine := theme.CachedActionTitle.Render(icon + " " + actionTitle)
			descLine := theme.CachedActionDesc.Render("  " + desc)
			return lipgloss.NewStyle().
				Width(gridWidth).
				Background(theme.SurfaceBg).
				BorderLeft(true).
				BorderStyle(lipgloss.ThickBorder()).
				BorderForeground(theme.Teal).
				PaddingLeft(1).
				Render(titleLine + "\n" + descLine)
		}
		titleLine := theme.CachedActionNormal.Render(icon + " " + actionTitle)
		descLine := theme.CachedActionDesc.Render("  " + desc)
		return lipgloss.NewStyle().
			PaddingLeft(2).
			Render(titleLine + "\n" + descLine)
	}

	newWorktreeItem := renderActionItem("+", "New Worktree", "Create worktree and session", g.Index == -1 && !g.InAvailable)
	listViewItem := renderActionItem("☰", "List View", "View all sessions", g.Index == -2 && !g.InAvailable)
	actionItems := lipgloss.NewStyle().MarginBottom(1).Render(newWorktreeItem + "\n" + listViewItem)
	gridSections = append(gridSections, actionItems)

	if len(sessionPanels) > 0 {
		gridSections = append(gridSections, renderSectionHeader("SESSIONS"))
		gridSections = append(gridSections, renderPanelGrid(sessionPanels, sessionIndices, false))
	}
	if len(recentPanels) > 0 {
		gridSections = append(gridSections, renderSectionHeader("RECENT"))
		gridSections = append(gridSections, renderPanelGrid(recentPanels, recentIndices, false))
	}
	if len(filteredPanels) == 0 && g.Filtering {
		gridSections = append(gridSections, lipgloss.NewStyle().Foreground(theme.DimColor).Render("No matching sessions"))
	}
	if len(filteredAvailable) > 0 {
		gridSections = append(gridSections, renderSectionHeader("AVAILABLE WORKTREES"))

		var availRows []string
		var availRow []string
		for i, panel := range filteredAvailable {
			isSelected := g.InAvailable && i == g.AvailIdx
			renderedPanel := renderPanel(panel, i, isSelected, false)
			availRow = append(availRow, renderedPanel)

			if len(availRow) >= g.Cols || i == len(filteredAvailable)-1 {
				availRows = append(availRows, lipgloss.JoinHorizontal(lipgloss.Top, availRow...))
				availRow = []string{}
			}
		}
		gridSections = append(gridSections, lipgloss.JoinVertical(lipgloss.Left, availRows...))
	}

	grid := lipgloss.JoinVertical(lipgloss.Left, gridSections...)
	headerHeight := 3
	footerHeight := 3
	availableHeight := height - headerHeight - footerHeight
	if availableHeight < 1 {
		availableHeight = 1
	}

	gridLines := strings.Split(grid, "\n")
	startLine := g.ScrollOffset
	if startLine < 0 {
		startLine = 0
	}
	if startLine > len(gridLines) {
		startLine = len(gridLines)
	}
	endLine := startLine + availableHeight
	if endLine > len(gridLines) {
		endLine = len(gridLines)
	}
	visibleGrid := strings.Join(gridLines[startLine:endLine], "\n")
	body := lipgloss.NewStyle().Height(availableHeight).MarginLeft(2).Render(visibleGrid)

	footer := lipgloss.NewStyle().
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.OverlayColor).
		Padding(0, 2).
		Render(hint)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (g *GridState) RenderDetail(width, height int) string {
	if g.DetailPanel == nil {
		return ""
	}
	panel := g.DetailPanel

	modalWidth := 60
	if width < 70 {
		modalWidth = width - 10
	}
	if modalWidth < 40 {
		modalWidth = 40
	}

	trafficRed := lipgloss.NewStyle().Foreground(theme.TrafficRed).Render("●")
	trafficYellow := lipgloss.NewStyle().Foreground(theme.TrafficYellow).Render("●")
	trafficGreen := lipgloss.NewStyle().Foreground(theme.TrafficGreen).Render("●")
	traffic := trafficRed + " " + trafficYellow + " " + trafficGreen

	titleBar := lipgloss.NewStyle().
		Width(modalWidth - 2).
		Background(theme.SurfaceBg).
		Padding(0, 1).
		Render(traffic + "  " + lipgloss.NewStyle().Foreground(theme.TextColor).Bold(true).Render(panel.Name))

	var infoLines []string

	if panel.Branch != "" {
		infoLines = append(infoLines, lipgloss.NewStyle().Foreground(theme.Accent2).Render("⎇ "+panel.Branch))
	}
	if panel.Path != "" {
		pathDisplay := panel.Path
		maxPath := modalWidth - 8
		if len(pathDisplay) > maxPath {
			pathDisplay = "…" + pathDisplay[len(pathDisplay)-maxPath+1:]
		}
		infoLines = append(infoLines, lipgloss.NewStyle().Foreground(theme.DimColor).Render("  "+pathDisplay))
	}

	if panel.HasSession {
		sessionInfo := lipgloss.NewStyle().Foreground(theme.SuccessColor).Render("● active")
		if panel.Windows > 0 {
			sessionInfo += lipgloss.NewStyle().Foreground(theme.DimColor).Render(fmt.Sprintf("  %d windows, %d panes", panel.Windows, panel.Panes))
		}
		infoLines = append(infoLines, sessionInfo)
	} else {
		infoLines = append(infoLines, lipgloss.NewStyle().Foreground(theme.DimColor).Render("○ no active session"))
	}

	if panel.Modified > 0 || panel.Staged > 0 {
		var parts []string
		if panel.Modified > 0 {
			parts = append(parts, lipgloss.NewStyle().Foreground(theme.WarnColor).Render(fmt.Sprintf("%d modified", panel.Modified)))
		}
		if panel.Staged > 0 {
			parts = append(parts, lipgloss.NewStyle().Foreground(theme.SuccessColor).Render(fmt.Sprintf("%d staged", panel.Staged)))
		}
		infoLines = append(infoLines, strings.Join(parts, "  "))
	}

	infoSection := lipgloss.NewStyle().
		Width(modalWidth - 4).
		Padding(1, 1).
		Render(strings.Join(infoLines, "\n"))

	divider := lipgloss.NewStyle().
		Foreground(theme.SurfaceBg).
		Render(strings.Repeat("─", modalWidth-2))

	type actionItem struct {
		label string
		key   string
	}
	var actions []actionItem
	if panel.HasSession {
		actions = append(actions, actionItem{"Jump to session", "enter"})
		if panel.IsOrphan {
			actions = append(actions, actionItem{"Adopt session", "a"})
			actions = append(actions, actionItem{"Kill session", "x"})
		} else {
			actions = append(actions, actionItem{"Kill session", "x"})
		}
	} else {
		actions = append(actions, actionItem{"Start session", "enter"})
	}
	actions = append(actions, actionItem{"Back", "esc"})

	var actionLines []string
	for i, action := range actions {
		isSelected := i == g.DetailIdx

		actionKeyStyle := lipgloss.NewStyle().
			Foreground(theme.SurfaceBg).
			Background(theme.DimColor).
			Padding(0, 1)

		labelStyle := lipgloss.NewStyle().Foreground(theme.DimColor)

		if isSelected {
			actionKeyStyle = actionKeyStyle.Background(theme.SuccessColor).Foreground(theme.BaseBg)
			labelStyle = labelStyle.Foreground(theme.TextColor).Bold(true)
		}

		line := actionKeyStyle.Render(action.key) + " " + labelStyle.Render(action.label)
		actionLines = append(actionLines, line)
	}

	actionSection := lipgloss.NewStyle().
		Width(modalWidth - 4).
		Padding(1, 1).
		Render(strings.Join(actionLines, "\n"))

	modalContent := lipgloss.JoinVertical(lipgloss.Left,
		titleBar,
		infoSection,
		divider,
		actionSection,
	)

	modal := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.OverlayColor).
		Render(modalContent)

	hintText := lipgloss.NewStyle().Foreground(theme.DimColor).Render("↑↓/tab navigate  enter confirm  esc back")

	modalWithHint := lipgloss.JoinVertical(lipgloss.Center, modal, "", hintText)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modalWithHint)
}
