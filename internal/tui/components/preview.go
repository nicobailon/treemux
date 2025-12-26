package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/nicobailon/treemux/internal/recent"
	"github.com/nicobailon/treemux/internal/scanner"
	"github.com/nicobailon/treemux/internal/tmux"
	"github.com/nicobailon/treemux/internal/tui/theme"
	"github.com/nicobailon/treemux/internal/workspace"
)

type TmuxClient interface {
	HasSession(name string) bool
	SessionInfo(name string) (*tmux.SessionInfo, error)
}

type ProcessStatus int

const (
	ProcessIdle ProcessStatus = iota
	ProcessRunning
	ProcessBuilding
	ProcessServer
)

var runningProcesses = map[string]ProcessStatus{
	"node":       ProcessServer,
	"npm":        ProcessBuilding,
	"yarn":       ProcessBuilding,
	"pnpm":       ProcessBuilding,
	"bun":        ProcessBuilding,
	"go":         ProcessBuilding,
	"cargo":      ProcessBuilding,
	"rustc":      ProcessBuilding,
	"python":     ProcessRunning,
	"python3":    ProcessRunning,
	"ruby":       ProcessRunning,
	"java":       ProcessRunning,
	"make":       ProcessBuilding,
	"webpack":    ProcessBuilding,
	"vite":       ProcessServer,
	"next":       ProcessServer,
	"esbuild":    ProcessBuilding,
	"tsc":        ProcessBuilding,
	"tsx":        ProcessServer,
	"nodemon":    ProcessServer,
	"uvicorn":    ProcessServer,
	"gunicorn":   ProcessServer,
	"flask":      ProcessServer,
	"django":     ProcessServer,
	"rails":      ProcessServer,
	"redis":      ProcessServer,
	"postgres":   ProcessServer,
	"mysql":      ProcessServer,
	"mongod":     ProcessServer,
	"docker":     ProcessServer,
	"kubectl":    ProcessRunning,
	"ssh":        ProcessRunning,
	"tmux":       ProcessIdle,
	"zsh":        ProcessIdle,
	"bash":       ProcessIdle,
	"fish":       ProcessIdle,
	"vim":        ProcessRunning,
	"nvim":       ProcessRunning,
	"code":       ProcessRunning,
	"cursor":     ProcessRunning,
	"emacs":      ProcessRunning,
	"nano":       ProcessRunning,
	"less":       ProcessIdle,
	"cat":        ProcessIdle,
	"grep":       ProcessIdle,
	"codex":      ProcessRunning,
	"claude":     ProcessRunning,
	"aider":      ProcessRunning,
	"pi":         ProcessRunning,
	"rg":         ProcessIdle,
	"fd":         ProcessIdle,
	"fzf":        ProcessIdle,
}

func ClassifyProcess(name string) ProcessStatus {
	if status, ok := runningProcesses[name]; ok {
		return status
	}
	return ProcessIdle
}

func (s ProcessStatus) Icon() string {
	switch s {
	case ProcessServer:
		return "●"
	case ProcessBuilding:
		return "◐"
	case ProcessRunning:
		return "◉"
	default:
		return "○"
	}
}

type ItemKind int

const (
	KindCreate ItemKind = iota
	KindGridView
	KindWorktree
	KindOrphan
	KindRecent
	KindGlobal
	KindRepoHeader
	KindHeader
	KindSeparator
)

type PreviewContext struct {
	Width           int
	PaneContent     string
	PaneSession     string
	GlobalMode      bool
	Tmux            TmuxClient
	States          []workspace.WorktreeState
	Orphans         []string
	GlobalWorktrees []scanner.RepoWorktree
}

type PreviewItem struct {
	Kind  ItemKind
	Title string
	Data  interface{}
}

func truncatePath(path string, maxLen int) string {
	if maxLen < 10 {
		maxLen = 10
	}
	if len(path) <= maxLen {
		return path
	}
	return "..." + path[len(path)-maxLen+3:]
}

func kvLine(key, value string) string {
	return theme.DimStyle.Render(fmt.Sprintf("%-8s", key)) + value
}

func renderCard(title string, content string, width int) string {
	cardWidth := width - 4
	if cardWidth < 20 {
		cardWidth = 20
	}

	titleBar := lipgloss.NewStyle().
		Foreground(theme.BaseBg).
		Background(theme.Accent).
		Bold(true).
		Width(cardWidth).
		Padding(0, 1).
		Render(title)

	cardBody := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Accent).
		BorderTop(false).
		Padding(0, 1).
		Width(cardWidth).
		Render(content)

	return titleBar + "\n" + cardBody
}

func renderCompactTerminal(ctx PreviewContext, maxLines int) string {
	if ctx.PaneContent == "" {
		return ""
	}

	boxWidth := ctx.Width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}

	trafficLights := lipgloss.NewStyle().Foreground(theme.ErrorColor).Render("●") + " " +
		lipgloss.NewStyle().Foreground(theme.WarnColor).Render("●") + " " +
		lipgloss.NewStyle().Foreground(theme.SuccessColor).Render("●")

	titleText := ctx.PaneSession
	if len(titleText) > boxWidth-15 {
		titleText = titleText[:boxWidth-18] + "..."
	}
	termTitleStyle := lipgloss.NewStyle().Foreground(theme.SubTextColor)

	titleBarContent := trafficLights + "  " + termTitleStyle.Render(titleText)
	titleBar := lipgloss.NewStyle().
		Background(theme.SurfaceBg).
		Width(boxWidth).
		Padding(0, 1).
		Render(titleBarContent)

	termStyle := lipgloss.NewStyle().Foreground(theme.TextColor)
	lines := strings.Split(ctx.PaneContent, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	var termLines []string
	maxWidth := boxWidth - 4
	if maxWidth < 20 {
		maxWidth = 20
	}
	for _, line := range lines {
		if len(line) > maxWidth {
			line = line[:maxWidth-3] + "..."
		}
		termLines = append(termLines, termStyle.Render(line))
	}

	contentStyle := lipgloss.NewStyle().
		Background(theme.BaseBg).
		Width(boxWidth).
		Padding(0, 1)
	termContent := contentStyle.Render(strings.Join(termLines, "\n"))

	return titleBar + "\n" + termContent
}

func renderCreatePreview(width int) string {
	title := theme.SectionStyle.Render(theme.IconCreate + " New Worktree")
	steps := []string{
		theme.SectionStyle.Render("1.") + " " + theme.TextStyle.Render("Select base branch"),
		theme.DimStyle.Render("2.") + " " + theme.TextStyle.Render("Create worktree"),
		theme.DimStyle.Render("3.") + " " + theme.TextStyle.Render("Start tmux session"),
	}
	stepsCard := renderCard("Workflow", strings.Join(steps, "\n"), width)
	hint := theme.DimStyle.Render("enter") + " " + theme.SubTextStyle.Render("begin")
	return title + "\n\n" + stepsCard + "\n\n" + hint
}

func renderGlobalCreatePreview(width int) string {
	title := theme.SectionStyle.Render(theme.IconCreate + " New Worktree")
	steps := []string{
		theme.SectionStyle.Render("1.") + " " + theme.TextStyle.Render("Select repository"),
		theme.DimStyle.Render("2.") + " " + theme.TextStyle.Render("Select base branch"),
		theme.DimStyle.Render("3.") + " " + theme.TextStyle.Render("Create worktree"),
		theme.DimStyle.Render("4.") + " " + theme.TextStyle.Render("Start tmux session"),
	}
	stepsCard := renderCard("Workflow", strings.Join(steps, "\n"), width)
	hint := theme.DimStyle.Render("enter") + " " + theme.SubTextStyle.Render("begin")
	return title + "\n\n" + stepsCard + "\n\n" + hint
}

func renderOrphanPreview(name string, width int) string {
	title := theme.WarnStyle.Render(theme.IconOrphan + " Orphaned Session")
	nameDisplay := truncatePath(name, width-8)
	infoLines := []string{
		kvLine("Session", theme.TextStyle.Render(nameDisplay)),
		kvLine("Status", theme.WarnStyle.Render("No matching worktree")),
	}
	infoCard := renderCard(theme.IconSession+" Details", strings.Join(infoLines, "\n"), width)
	hint := theme.DimStyle.Render("enter") + " " + theme.SubTextStyle.Render("jump") + "  " +
		theme.DimStyle.Render("tab") + " " + theme.SubTextStyle.Render("actions")
	return title + "\n\n" + infoCard + "\n\n" + hint
}

func renderRecentPreview(r recent.Entry, width int) string {
	title := theme.SectionStyle.Render(theme.IconPath + " " + r.RepoName)

	maxW := width - 12
	if maxW < 20 {
		maxW = 20
	}

	worktree := r.Worktree
	if maxW > 4 && len(worktree) > maxW {
		worktree = worktree[:maxW-2] + ".."
	}

	pathDisplay := r.Path
	if len(pathDisplay) > maxW {
		pathDisplay = "..." + pathDisplay[len(pathDisplay)-maxW+3:]
	}

	infoLines := []string{
		kvLine("Worktree", theme.TextStyle.Render(worktree)),
		kvLine("Session", theme.TextStyle.Render(r.SessionName)),
		kvLine("Path", theme.SubTextStyle.Render(pathDisplay)),
	}
	infoCard := renderCard(theme.IconBranch+" Details", strings.Join(infoLines, "\n"), width)
	hint := theme.DimStyle.Render("enter") + " " + theme.SubTextStyle.Render("switch to session")
	return title + "\n\n" + infoCard + "\n\n" + hint
}

func renderWorktreePreview(wt workspace.WorktreeState, width int) string {
	maxW := width - 12
	if maxW < 20 {
		maxW = 20
	}

	title := theme.CurrentStyle.Render(theme.IconWorktree + " " + wt.Worktree.Name)

	pathDisplay := wt.Worktree.Path
	if len(pathDisplay) > maxW {
		pathDisplay = "..." + pathDisplay[len(pathDisplay)-maxW+3:]
	}

	statusText := "unknown"
	if wt.Status != nil {
		if wt.Status.Clean {
			statusText = theme.SuccessStyle.Render(theme.IconClean + " clean")
		} else {
			parts := []string{}
			if wt.Status.Modified > 0 {
				parts = append(parts, theme.WarnStyle.Render(fmt.Sprintf("%d modified", wt.Status.Modified)))
			}
			if wt.Status.Staged > 0 {
				parts = append(parts, theme.SectionStyle.Render(fmt.Sprintf("%d staged", wt.Status.Staged)))
			}
			if wt.Status.Untracked > 0 {
				parts = append(parts, theme.DimStyle.Render(fmt.Sprintf("%d untracked", wt.Status.Untracked)))
			}
			statusText = strings.Join(parts, ", ")
		}
	}

	statusLines := []string{
		kvLine("Branch", theme.TextStyle.Render(wt.Worktree.Branch)),
		kvLine("Status", statusText),
		kvLine("Path", theme.SubTextStyle.Render(pathDisplay)),
	}

	if wt.Ahead > 0 || wt.Behind > 0 {
		sync := ""
		if wt.Ahead > 0 {
			sync += theme.SuccessStyle.Render(fmt.Sprintf("%d ahead", wt.Ahead))
		}
		if wt.Behind > 0 {
			if sync != "" {
				sync += ", "
			}
			sync += theme.WarnStyle.Render(fmt.Sprintf("%d behind", wt.Behind))
		}
		statusLines = append(statusLines, kvLine("Sync", sync))
	}

	statusCard := renderCard(theme.IconBranch+" Status", strings.Join(statusLines, "\n"), width)

	var sessionCard string
	if wt.SessionInfo != nil {
		sessionLines := []string{}
		sessionInfo := fmt.Sprintf("%d windows, %d panes", wt.SessionInfo.Windows, wt.SessionInfo.Panes)
		if wt.SessionInfo.IsActive {
			sessionInfo += " " + theme.SuccessStyle.Render("● active")
		}
		sessionLines = append(sessionLines, theme.TextStyle.Render(sessionInfo))

		if len(wt.Processes) > 0 && len(wt.Processes) <= 3 {
			sessionLines = append(sessionLines, "")
			for _, p := range wt.Processes {
				status := ClassifyProcess(p)
				var style lipgloss.Style
				switch status {
				case ProcessServer:
					style = theme.SuccessStyle
				case ProcessBuilding:
					style = theme.WarnStyle
				default:
					style = theme.SubTextStyle
				}
				sessionLines = append(sessionLines, style.Render(status.Icon()+" "+p))
			}
		}
		sessionCard = renderCard(theme.IconSession+" Session", strings.Join(sessionLines, "\n"), width)
	}

	hint := theme.DimStyle.Render("enter") + " " + theme.SubTextStyle.Render("jump") + "  " +
		theme.DimStyle.Render("tab") + " " + theme.SubTextStyle.Render("actions")

	sections := []string{title, "", statusCard}
	if sessionCard != "" {
		sections = append(sections, "", sessionCard)
	}
	sections = append(sections, "", hint)

	return strings.Join(sections, "\n")
}

func renderGlobalPreview(wt scanner.RepoWorktree, ctx PreviewContext) string {
	maxW := ctx.Width - 12
	if maxW < 20 {
		maxW = 20
	}

	title := theme.SectionStyle.Render(theme.IconPath + " " + wt.RepoName + "/" + wt.Worktree.Name)

	pathDisplay := wt.Worktree.Path
	if len(pathDisplay) > maxW {
		pathDisplay = "..." + pathDisplay[len(pathDisplay)-maxW+3:]
	}

	hasSession := ctx.Tmux.HasSession(wt.Worktree.Name)

	statusLines := []string{
		kvLine("Branch", theme.TextStyle.Render(wt.Worktree.Branch)),
		kvLine("Path", theme.SubTextStyle.Render(pathDisplay)),
	}

	if hasSession {
		statusLines = append(statusLines, kvLine("Session", theme.SuccessStyle.Render("● active")))
		if info, err := ctx.Tmux.SessionInfo(wt.Worktree.Name); err == nil && info != nil {
			sessionInfo := fmt.Sprintf("%d windows, %d panes", info.Windows, info.Panes)
			statusLines = append(statusLines, kvLine("", theme.TextStyle.Render(sessionInfo)))
		}
	} else {
		statusLines = append(statusLines, kvLine("Session", theme.DimStyle.Render("○ inactive")))
	}

	statusCard := renderCard(theme.IconBranch+" Worktree", strings.Join(statusLines, "\n"), ctx.Width)

	workflowTitle := "Workflow"
	var workflowLines []string
	if hasSession {
		workflowLines = []string{
			theme.TextStyle.Render("1. Jump to tmux session"),
			theme.TextStyle.Render("2. Continue working"),
		}
	} else {
		workflowLines = []string{
			theme.TextStyle.Render("1. Start tmux session"),
			theme.TextStyle.Render("2. Begin working"),
		}
	}
	workflowCard := renderCard(theme.IconSession+" "+workflowTitle, strings.Join(workflowLines, "\n"), ctx.Width)

	var hint string
	if hasSession {
		hint = theme.DimStyle.Render("enter") + " " + theme.SubTextStyle.Render("jump to session")
	} else {
		hint = theme.DimStyle.Render("enter") + " " + theme.SubTextStyle.Render("start session")
	}

	return strings.Join([]string{title, "", statusCard, "", workflowCard, "", hint}, "\n")
}

func renderGridViewPreview(ctx PreviewContext) string {
	title := theme.SectionStyle.Render("◫ Grid View")

	sessionCount := 0
	if ctx.GlobalMode {
		for _, wt := range ctx.GlobalWorktrees {
			if ctx.Tmux.HasSession(wt.Worktree.Name) {
				sessionCount++
			}
		}
	} else {
		for _, st := range ctx.States {
			if st.HasSession {
				sessionCount++
			}
		}
	}
	sessionCount += len(ctx.Orphans)

	infoLines := []string{
		kvLine("Sessions", theme.TextStyle.Render(fmt.Sprintf("%d active", sessionCount))),
	}
	infoCard := renderCard("Overview", strings.Join(infoLines, "\n"), ctx.Width)

	controls := []string{
		theme.DimStyle.Render("arrows") + " " + theme.TextStyle.Render("navigate"),
		theme.DimStyle.Render("tab") + " " + theme.TextStyle.Render("cycle"),
		theme.DimStyle.Render("enter") + " " + theme.TextStyle.Render("jump"),
	}
	controlsCard := renderCard("Controls", strings.Join(controls, "\n"), ctx.Width)

	hint := theme.DimStyle.Render("enter") + " " + theme.SubTextStyle.Render("open grid view")
	return title + "\n\n" + infoCard + "\n\n" + controlsCard + "\n\n" + hint
}

func RenderPreview(ctx PreviewContext, item PreviewItem) string {
	var infoContent string
	switch item.Kind {
	case KindCreate:
		if ctx.GlobalMode {
			infoContent = renderGlobalCreatePreview(ctx.Width)
		} else {
			infoContent = renderCreatePreview(ctx.Width)
		}
	case KindOrphan:
		infoContent = renderOrphanPreview(item.Title, ctx.Width)
	case KindWorktree:
		wt := item.Data.(workspace.WorktreeState)
		infoContent = renderWorktreePreview(wt, ctx.Width)
	case KindRecent:
		r := item.Data.(recent.Entry)
		infoContent = renderRecentPreview(r, ctx.Width)
	case KindGlobal:
		wt := item.Data.(scanner.RepoWorktree)
		infoContent = renderGlobalPreview(wt, ctx)
	case KindGridView:
		infoContent = renderGridViewPreview(ctx)
	default:
		infoContent = ""
	}

	terminalPreview := renderCompactTerminal(ctx, 20)
	if terminalPreview != "" {
		return terminalPreview + "\n\n" + infoContent
	}
	return infoContent
}
