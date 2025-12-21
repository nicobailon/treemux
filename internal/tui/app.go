package tui

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nicobailon/treemux/internal/config"
	"github.com/nicobailon/treemux/internal/git"
	"github.com/nicobailon/treemux/internal/recent"
	"github.com/nicobailon/treemux/internal/scanner"
	"github.com/nicobailon/treemux/internal/tmux"
	"github.com/nicobailon/treemux/internal/workspace"
)

type viewState int

const (
	stateMain viewState = iota
	stateCreateName
	stateCreateBranch
	stateOrphanBranch
	stateActionMenu
	stateOrphanMenu
	stateHelp
)

type itemKind int

const (
	kindCreate itemKind = iota
	kindWorktree
	kindOrphan
	kindRecent
	kindGlobal
	kindRepoHeader
	kindHeader
	kindSeparator
)

type listItem struct {
	title     string
	desc      string
	kind      itemKind
	data      interface{}
	isCurrent bool
}

func (i listItem) Title() string       { return i.title }
func (i listItem) Description() string { return i.desc }
func (i listItem) FilterValue() string { return i.title }

type dataLoadedMsg struct {
	states  []workspace.WorktreeState
	orphans []string
}

type globalDataLoadedMsg struct {
	worktrees []scanner.RepoWorktree
	orphans   []string
}

type branchesMsg struct {
	branches []string
}

type resultMsg struct {
	action string
	err    error
}

type model struct {
	svc             *workspace.Service
	cfg             *config.Config
	tmux            *tmux.Tmux
	recentStore     *recent.Store
	state           viewState
	nextBranchState viewState
	list            list.Model
	preview         viewport.Model
	input           textinput.Model
	menu            list.Model
	spinner         spinner.Model
	states          []workspace.WorktreeState
	orphans         []string
	recentEntries   []recent.Entry
	globalWorktrees []scanner.RepoWorktree
	width           int
	height          int
	err             error
	pending         string
	pendingWT       *workspace.WorktreeState
	pendingGlobal   *scanner.RepoWorktree
	loading         bool
	filtering       bool
	jumpTarget      *JumpTarget
	globalMode      bool
	inGitRepo       bool
}

type JumpTarget struct {
	SessionName string
	Path        string
}

type App struct {
	svc       *workspace.Service
	cfg       *config.Config
	tmux      *tmux.Tmux
	inGitRepo bool
}

func New(svc *workspace.Service, cfg *config.Config, t *tmux.Tmux, inGitRepo bool) *App {
	return &App{svc: svc, cfg: cfg, tmux: t, inGitRepo: inGitRepo}
}

func (a *App) Run() (*JumpTarget, error) {
	m := initialModel(a.svc, a.cfg, a.tmux, a.inGitRepo)
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return nil, err
	}
	if fm, ok := finalModel.(model); ok && fm.jumpTarget != nil {
		return fm.jumpTarget, nil
	}
	return nil, nil
}

func initialModel(svc *workspace.Service, cfg *config.Config, t *tmux.Tmux, inGitRepo bool) model {
	del := newItemDelegate(50)
	l := list.New([]list.Item{}, del, 0, 0)
	l.DisableQuitKeybindings()
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.FilterInput.Prompt = "/ "
	l.FilterInput.PromptStyle = keyStyle
	l.FilterInput.TextStyle = textStyle

	ti := textinput.New()
	ti.CharLimit = 64
	ti.Placeholder = "worktree-name"
	ti.Prompt = ""
	ti.TextStyle = textStyle
	ti.PlaceholderStyle = subTextStyle

	menuDel := list.NewDefaultDelegate()
	menuDel.ShowDescription = true
	menuDel.Styles.NormalTitle = textStyle
	menuDel.Styles.NormalDesc = dimStyle
	menuDel.Styles.SelectedTitle = currentStyle
	menuDel.Styles.SelectedDesc = sectionStyle
	menu := list.New([]list.Item{}, menuDel, 0, 0)
	menu.DisableQuitKeybindings()
	menu.SetShowHelp(false)
	menu.SetShowStatusBar(false)
	menu.SetFilteringEnabled(false)
	menu.SetShowTitle(false)
	menu.SetShowPagination(false)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)

	vp := viewport.New(0, 0)

	recentStore, _ := recent.Load()

	return model{
		svc:             svc,
		cfg:             cfg,
		tmux:            t,
		recentStore:     recentStore,
		state:           stateMain,
		nextBranchState: stateCreateBranch,
		list:            l,
		preview:         vp,
		input:           ti,
		menu:            menu,
		spinner:         sp,
		loading:         true,
		globalMode:      !inGitRepo,
		inGitRepo:       inGitRepo,
	}
}

// TEA plumbing

func (m model) Init() tea.Cmd {
	if m.globalMode {
		return tea.Batch(m.spinner.Tick, loadGlobalDataCmd(m.cfg, m.tmux))
	}
	return tea.Batch(m.spinner.Tick, loadDataCmd(m.svc))
}

func loadDataCmd(svc *workspace.Service) tea.Cmd {
	return func() tea.Msg {
		states, orphans, err := svc.List()
		if err != nil {
			return resultMsg{action: "load", err: err}
		}
		return dataLoadedMsg{states: states, orphans: orphans}
	}
}

func loadGlobalDataCmd(cfg *config.Config, t *tmux.Tmux) tea.Cmd {
	return func() tea.Msg {
		worktrees := scanner.ScanAll(cfg.SearchPaths)
		sessions, _ := t.ListSessions()
		sessionSet := make(map[string]bool)
		for _, s := range sessions {
			sessionSet[s.Name] = true
		}
		wtNames := make(map[string]bool)
		for _, wt := range worktrees {
			wtNames[wt.Worktree.Name] = true
		}
		var orphans []string
		for name := range sessionSet {
			if !wtNames[name] {
				orphans = append(orphans, name)
			}
		}
		return globalDataLoadedMsg{worktrees: worktrees, orphans: orphans}
	}
}

func branchesCmd(svc *workspace.Service) tea.Cmd {
	return func() tea.Msg {
		branches, err := svc.Git.Branches()
		if err != nil {
			return resultMsg{action: "branches", err: err}
		}
		// put default branch first
		def := svc.Git.DefaultBranch()
		sort.Strings(branches)
		if def != "" {
			branches = append([]string{def}, filterStrings(branches, def)...)
		}
		return branchesMsg{branches: branches}
	}
}

func createWorktreeCmd(svc *workspace.Service, name, branch string) tea.Cmd {
	return func() tea.Msg {
		_, err := svc.CreateWorktree(name, branch)
		return resultMsg{action: "create", err: err}
	}
}

func deleteWorktreeCmd(svc *workspace.Service, path string) tea.Cmd {
	return func() tea.Msg {
		err := svc.DeleteWorktree(path, true)
		return resultMsg{action: "delete", err: err}
	}
}

func killSessionCmd(svc *workspace.Service, name string) tea.Cmd {
	return func() tea.Msg {
		err := svc.KillSession(name)
		return resultMsg{action: "kill-session", err: err}
	}
}

func killSessionDirectCmd(t *tmux.Tmux, name string) tea.Cmd {
	return func() tea.Msg {
		err := t.KillSession(name)
		return resultMsg{action: "kill-session", err: err}
	}
}

type jumpMsg struct {
	sessionName string
	path        string
	repoRoot    string
	worktree    string
}

func jumpCmd(svc *workspace.Service, name, path string, store *recent.Store) tea.Cmd {
	return func() tea.Msg {
		sessionName := svc.SessionName(path)
		if !svc.Tmux.HasSession(sessionName) {
			if err := svc.Tmux.NewSession(sessionName, path); err != nil {
				return resultMsg{action: "jump", err: err}
			}
		}
		if store != nil {
			store.Add(svc.Git.RepoRoot, name, sessionName, path)
			_ = store.Save()
		}
		return jumpMsg{sessionName: sessionName, path: path, repoRoot: svc.Git.RepoRoot, worktree: name}
	}
}

func switchRecentCmd(svc *workspace.Service, entry recent.Entry, store *recent.Store) tea.Cmd {
	return func() tea.Msg {
		if store != nil {
			store.Add(entry.RepoRoot, entry.Worktree, entry.SessionName, entry.Path)
			_ = store.Save()
		}
		return jumpMsg{sessionName: entry.SessionName, path: entry.Path, repoRoot: entry.RepoRoot, worktree: entry.Worktree}
	}
}

func switchSessionCmd(svc *workspace.Service, name string) tea.Cmd {
	return func() tea.Msg {
		return jumpMsg{sessionName: name}
	}
}

func adoptCmd(svc *workspace.Service, name, branch string) tea.Cmd {
	return func() tea.Msg {
		_, err := svc.AdoptOrphan(name, branch)
		return resultMsg{action: "adopt", err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		listWidth := int(float64(msg.Width) * 0.45)
		if listWidth < 30 {
			listWidth = 30
		}
		previewWidth := msg.Width - listWidth - 4
		headerHeight := 6
		m.list.SetDelegate(newItemDelegate(listWidth))
		m.list.SetSize(listWidth, msg.Height-headerHeight-2)
		m.menu.SetSize(msg.Width-6, msg.Height-6)
		m.preview.Width = previewWidth
		m.preview.Height = msg.Height - headerHeight - 2
		return m, nil

	case dataLoadedMsg:
		m.loading = false
		m.states = reorderCurrentFirst(msg.states, m.svc.Git.RepoRoot)
		m.orphans = msg.orphans
		if m.recentStore != nil {
			m.recentEntries = m.recentStore.GetOtherProjects(m.svc.Git.RepoRoot, 5)
		}
		m.list.SetItems(buildItems(m.states, m.orphans, m.recentEntries, m.svc.Git.RepoRoot))
		m.updatePreview()
		return m, nil

	case globalDataLoadedMsg:
		m.loading = false
		m.globalWorktrees = msg.worktrees
		m.orphans = msg.orphans
		m.list.SetItems(buildGlobalItems(m.globalWorktrees, m.orphans))
		m.updatePreview()
		return m, nil

	case branchesMsg:
		items := []list.Item{}
		for _, b := range msg.branches {
			items = append(items, listItem{title: b, desc: "base branch", kind: kindWorktree})
		}
		m.menu.SetItems(items)
		m.menu.Select(0)
		m.state = m.nextBranchState
		return m, nil

	case jumpMsg:
		m.jumpTarget = &JumpTarget{SessionName: msg.sessionName, Path: msg.path}
		return m, tea.Quit

	case resultMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil
		}
		switch msg.action {
		case "create", "delete", "kill-session", "adopt":
			m.state = stateMain
			if m.globalMode {
				return m, loadGlobalDataCmd(m.cfg, m.tmux)
			}
			return m, loadDataCmd(m.svc)
		}
		return m, nil
	}

	// state-specific handling
	switch m.state {
	case stateCreateName:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				name := strings.TrimSpace(m.input.Value())
				if name == "" {
					return m, nil
				}
				m.pending = name
				m.nextBranchState = stateCreateBranch
				return m, branchesCmd(m.svc)
			case "esc":
				m.state = stateMain
				return m, nil
			}
		}
		return m, cmd

	case stateCreateBranch:
		var cmd tea.Cmd
		m.menu, cmd = m.menu.Update(msg)
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				if sel, ok := m.menu.SelectedItem().(listItem); ok {
					branch := sel.title
					name := m.pending
					m.state = stateMain
					return m, tea.Batch(createWorktreeCmd(m.svc, name, branch), loadDataCmd(m.svc))
				}
			case "esc":
				m.state = stateMain
			}
		}
		return m, cmd

	case stateOrphanBranch:
		var cmd tea.Cmd
		m.menu, cmd = m.menu.Update(msg)
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				if sel, ok := m.menu.SelectedItem().(listItem); ok {
					branch := sel.title
					name := m.pending
					m.state = stateMain
					return m, tea.Batch(adoptCmd(m.svc, name, branch), loadDataCmd(m.svc))
				}
			case "esc":
				m.state = stateMain
			}
		}
		return m, cmd

	case stateActionMenu, stateOrphanMenu:
		var cmd tea.Cmd
		m.menu, cmd = m.menu.Update(msg)
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				item := m.menu.SelectedItem().(listItem)
				title := item.title
				switch {
				case strings.Contains(title, "Jump"):
					if m.pendingWT != nil {
						sessionName := m.svc.SessionName(m.pendingWT.Worktree.Path)
						if !m.svc.Tmux.HasSession(sessionName) {
							_ = m.svc.Tmux.NewSession(sessionName, m.pendingWT.Worktree.Path)
						}
						if m.recentStore != nil {
							m.recentStore.Add(m.svc.Git.RepoRoot, m.pendingWT.Worktree.Name, sessionName, m.pendingWT.Worktree.Path)
							_ = m.recentStore.Save()
						}
						m.jumpTarget = &JumpTarget{SessionName: sessionName, Path: m.pendingWT.Worktree.Path}
						return m, tea.Quit
					}
					if m.pendingGlobal != nil {
						sessionName := m.pendingGlobal.Worktree.Name
						if !m.tmux.HasSession(sessionName) {
							_ = m.tmux.NewSession(sessionName, m.pendingGlobal.Worktree.Path)
						}
						if m.recentStore != nil {
							m.recentStore.Add(m.pendingGlobal.RepoRoot, m.pendingGlobal.Worktree.Name, sessionName, m.pendingGlobal.Worktree.Path)
							_ = m.recentStore.Save()
						}
						m.jumpTarget = &JumpTarget{SessionName: sessionName, Path: m.pendingGlobal.Worktree.Path}
						return m, tea.Quit
					}
					if m.pending != "" {
						m.jumpTarget = &JumpTarget{SessionName: m.pending}
						return m, tea.Quit
					}
				case strings.Contains(title, "Delete worktree"):
					if m.pendingWT != nil {
						if m.pendingWT.Worktree.Path == m.svc.Git.RepoRoot {
							m.err = fmt.Errorf("cannot delete current worktree")
							m.state = stateMain
							return m, nil
						}
						return m, deleteWorktreeCmd(m.svc, m.pendingWT.Worktree.Path)
					}
				case strings.Contains(title, "Kill session"):
					if m.pendingWT != nil {
						return m, killSessionCmd(m.svc, m.pendingWT.SessionName)
					}
					if m.pending != "" {
						if m.globalMode {
							return m, killSessionDirectCmd(m.tmux, m.pending)
						}
						return m, killSessionCmd(m.svc, m.pending)
					}
				case strings.Contains(title, "Adopt"):
					if m.pending != "" {
						m.nextBranchState = stateOrphanBranch
						return m, branchesCmd(m.svc)
					}
				}
				m.state = stateMain
				return m, nil
			case "esc":
				m.state = stateMain
				return m, nil
			}
		}
		return m, cmd
	}

	// main view handling
	var cmds []tea.Cmd
	var cmd tea.Cmd

	// Handle navigation to skip non-selectable items
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "j", "down":
			m.list, cmd = m.list.Update(msg)
			cmds = append(cmds, cmd)
			m.skipNonSelectable(1)
			m.updatePreview()
			return m, tea.Batch(cmds...)
		case "k", "up":
			m.list, cmd = m.list.Update(msg)
			cmds = append(cmds, cmd)
			m.skipNonSelectable(-1)
			m.updatePreview()
			return m, tea.Batch(cmds...)
		}
	}

	m.list, cmd = m.list.Update(msg)
	cmds = append(cmds, cmd)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.state == stateMain {
				return m, tea.Quit
			}
		case "?":
			if m.state == stateHelp {
				m.state = stateMain
			} else {
				m.state = stateHelp
			}
		case "g":
			if m.state == stateMain {
				m.globalMode = !m.globalMode
				m.loading = true
				if m.globalMode {
					return m, tea.Batch(m.spinner.Tick, loadGlobalDataCmd(m.cfg, m.tmux))
				}
				if m.inGitRepo {
					return m, tea.Batch(m.spinner.Tick, loadDataCmd(m.svc))
				}
				m.globalMode = true
				return m, tea.Batch(m.spinner.Tick, loadGlobalDataCmd(m.cfg, m.tmux))
			}
		case "enter":
			if sel, ok := m.list.SelectedItem().(listItem); ok {
				switch sel.kind {
				case kindCreate:
					if m.globalMode {
						return m, nil
					}
					m.state = stateCreateName
					m.input.SetValue("")
					m.input.Focus()
				case kindWorktree:
					wt := sel.data.(workspace.WorktreeState)
					m.pendingWT = &wt
					m.menu.SetItems(actionMenuItems(wt.HasSession))
					m.menu.Select(0)
					m.state = stateActionMenu
				case kindOrphan:
					m.pending = sel.title
					if m.globalMode {
						m.menu.SetItems(globalOrphanMenuItems())
					} else {
						m.menu.SetItems(orphanMenuItems())
					}
					m.menu.Select(0)
					m.state = stateOrphanMenu
				case kindRecent:
					r := sel.data.(recent.Entry)
					if m.recentStore != nil {
						m.recentStore.Add(r.RepoRoot, r.Worktree, r.SessionName, r.Path)
						_ = m.recentStore.Save()
					}
					m.jumpTarget = &JumpTarget{SessionName: r.SessionName, Path: r.Path}
					return m, tea.Quit
				case kindGlobal:
					wt := sel.data.(scanner.RepoWorktree)
					m.pendingGlobal = &wt
					m.menu.SetItems(globalActionMenuItems())
					m.menu.Select(0)
					m.state = stateActionMenu
				}
			}
		case "tab":
			if sel, ok := m.list.SelectedItem().(listItem); ok && sel.kind == kindWorktree {
				wt := sel.data.(workspace.WorktreeState)
				m.pendingWT = &wt
				m.menu.SetItems(actionMenuItems(wt.HasSession))
				m.menu.Select(0)
				m.state = stateActionMenu
			}
		case "ctrl+d":
			if sel, ok := m.list.SelectedItem().(listItem); ok && sel.kind == kindWorktree {
				wt := sel.data.(workspace.WorktreeState)
				if wt.Worktree.Path == m.svc.Git.RepoRoot {
					m.err = fmt.Errorf("cannot delete current worktree")
				} else {
					cmds = append(cmds, deleteWorktreeCmd(m.svc, wt.Worktree.Path))
				}
			}
		}
	case tea.MouseMsg:
		// not used
	}

	m.updatePreview()
	return m, tea.Batch(cmds...)
}

// Skip separator and header items during navigation
func (m *model) skipNonSelectable(direction int) {
	items := m.list.Items()
	idx := m.list.Index()
	for {
		if idx < 0 || idx >= len(items) {
			break
		}
		item, ok := items[idx].(listItem)
		if !ok {
			break
		}
		if item.kind != kindSeparator && item.kind != kindHeader && item.kind != kindRepoHeader {
			break
		}
		idx += direction
	}
	if idx >= 0 && idx < len(items) {
		m.list.Select(idx)
	}
}

// Preview rendering

func (m *model) updatePreview() {
	sel, ok := m.list.SelectedItem().(listItem)
	if !ok {
		return
	}
	switch sel.kind {
	case kindCreate:
		if m.globalMode {
			m.preview.SetContent(renderGlobalCreatePreview())
		} else {
			m.preview.SetContent(renderCreatePreview())
		}
	case kindOrphan:
		name := sel.title
		m.preview.SetContent(renderOrphanPreview(name))
	case kindWorktree:
		wt := sel.data.(workspace.WorktreeState)
		m.preview.SetContent(renderWorktreePreview(wt))
	case kindRecent:
		r := sel.data.(recent.Entry)
		m.preview.SetContent(renderRecentPreview(r))
	case kindGlobal:
		wt := sel.data.(scanner.RepoWorktree)
		m.preview.SetContent(renderGlobalWorktreePreview(wt))
	default:
		m.preview.SetContent("")
	}
}

// View

func (m model) View() string {
	if m.loading {
		loadingBox := lipgloss.NewStyle().
			Padding(2, 4).
			Render(m.spinner.View() + " Loading worktrees...")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, loadingBox)
	}

	if m.state == stateHelp {
		return renderHelp()
	}

	switch m.state {
	case stateCreateName:
		return renderPrompt("Create new worktree", "Name:", m.input.View())
	case stateCreateBranch:
		return renderMenu("Base branch", &m.menu)
	case stateOrphanBranch:
		return renderMenu("Adopt: base branch", &m.menu)
	case stateActionMenu:
		return renderMenu("Actions", &m.menu)
	case stateOrphanMenu:
		return renderMenu("Orphaned session", &m.menu)
	}

	left := listFrameStyle.Render(m.list.View())
	right := previewFrameStyle.Render(m.preview.View())
	if m.err != nil {
		right = errorStyle.Render(" " + m.err.Error()) + "\n\n" + right
	}

	logoStyle := lipgloss.NewStyle().Foreground(successColor).Bold(true)
	t1 := lipgloss.NewStyle().Foreground(lipgloss.Color("#f5c2e7")).Bold(true)
	t2 := lipgloss.NewStyle().Foreground(lipgloss.Color("#cba6f7")).Bold(true)
	t3 := lipgloss.NewStyle().Foreground(lipgloss.Color("#b4befe")).Bold(true)
	t4 := lipgloss.NewStyle().Foreground(lipgloss.Color("#89b4fa")).Bold(true)

	gradientTitle := logoStyle.Render("▲ ") +
		t1.Render("tre") +
		t2.Render("em") +
		t3.Render("u") +
		t4.Render("x")

	modeIndicator := ""
	if m.globalMode {
		modeIndicator = "  " + warnStyle.Render("[GLOBAL]")
	}

	headerBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(overlayColor).
		Padding(0, 2).
		MarginTop(1).
		Width(m.width - 4).
		Render(gradientTitle + "  " + dimStyle.Render("git worktrees + tmux sessions") + modeIndicator)

	toggleHint := keyStyle.Render("g") + dimStyle.Render(" global  ")
	if m.globalMode {
		toggleHint = keyStyle.Render("g") + dimStyle.Render(" repo  ")
	}

	footer := footerStyle.Render(
		keyStyle.Render("enter") + dimStyle.Render(" select  ") +
			keyStyle.Render("/") + dimStyle.Render(" filter  ") +
			toggleHint +
			keyStyle.Render("?") + dimStyle.Render(" help  ") +
			keyStyle.Render("q") + dimStyle.Render(" quit"),
	)

	sepHeight := m.height - 7
	if sepHeight < 1 {
		sepHeight = 1
	}
	separator := lipgloss.NewStyle().
		Foreground(overlayColor).
		Render(strings.Repeat("│\n", sepHeight))
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, separator, right)
	return lipgloss.JoinVertical(lipgloss.Left,
		headerBox,
		body,
		footer,
	)
}

// Helpers

func buildItems(states []workspace.WorktreeState, orphans []string, recentEntries []recent.Entry, currentPath string) []list.Item {
	items := []list.Item{}
	items = append(items, listItem{
		title: "+ Create new worktree ...",
		kind:  kindCreate,
	})
	for _, st := range states {
		items = append(items, listItem{
			title:     st.Worktree.Name,
			kind:      kindWorktree,
			data:      st,
			isCurrent: st.Worktree.Path == currentPath,
		})
	}
	if len(recentEntries) > 0 {
		items = append(items, listItem{kind: kindSeparator})
		items = append(items, listItem{title: "RECENT", kind: kindHeader})
		for _, r := range recentEntries {
			items = append(items, listItem{
				title: r.RepoName + "/" + r.Worktree,
				kind:  kindRecent,
				data:  r,
			})
		}
	}
	if len(orphans) > 0 {
		items = append(items, listItem{kind: kindSeparator})
		items = append(items, listItem{title: "ORPHANED SESSIONS", kind: kindHeader})
		for _, o := range orphans {
			items = append(items, listItem{
				title: o,
				kind:  kindOrphan,
				data:  o,
			})
		}
	}
	return items
}

func buildGlobalItems(worktrees []scanner.RepoWorktree, orphans []string) []list.Item {
	items := []list.Item{}
	items = append(items, listItem{
		title: "+ Create new worktree ... (switch to repo view)",
		kind:  kindCreate,
	})

	currentRepo := ""
	for _, wt := range worktrees {
		if wt.RepoName != currentRepo {
			if currentRepo != "" {
				items = append(items, listItem{kind: kindSeparator})
			}
			items = append(items, listItem{title: wt.RepoName, kind: kindRepoHeader})
			currentRepo = wt.RepoName
		}
		items = append(items, listItem{
			title: wt.Worktree.Name,
			desc:  wt.Worktree.Branch,
			kind:  kindGlobal,
			data:  wt,
		})
	}

	if len(orphans) > 0 {
		items = append(items, listItem{kind: kindSeparator})
		items = append(items, listItem{title: "ORPHANED SESSIONS", kind: kindHeader})
		for _, o := range orphans {
			items = append(items, listItem{
				title: o,
				kind:  kindOrphan,
				data:  o,
			})
		}
	}
	return items
}

func reorderCurrentFirst(states []workspace.WorktreeState, currentPath string) []workspace.WorktreeState {
	head := []workspace.WorktreeState{}
	tail := []workspace.WorktreeState{}
	for _, st := range states {
		if st.Worktree.Path == currentPath {
			head = append(head, st)
		} else {
			tail = append(tail, st)
		}
	}
	return append(head, tail...)
}

func renderWorktreePreview(wt workspace.WorktreeState) string {
	lines := []string{
		sectionTitle(iconPath + " Path"),
		dimStyle.Render(wt.Worktree.Path),
		"",
		sectionTitle(iconBranch + " Branch"),
		textStyle.Render(wt.Worktree.Branch),
		"",
		sectionTitle("Status"),
		statusLine(wt.Status),
	}
	if wt.Ahead > 0 || wt.Behind > 0 {
		syncInfo := ""
		if wt.Ahead > 0 {
			syncInfo += successStyle.Render(fmt.Sprintf("%s %d ", iconAhead, wt.Ahead))
		}
		if wt.Behind > 0 {
			syncInfo += warnStyle.Render(fmt.Sprintf("%s %d", iconBehind, wt.Behind))
		}
		lines = append(lines, syncInfo)
	}
	if wt.SessionInfo != nil {
		lines = append(lines, "", sectionTitle(iconSession+" Session"))
		sessionStatus := fmt.Sprintf("Windows: %d  Panes: %d", wt.SessionInfo.Windows, wt.SessionInfo.Panes)
		if wt.SessionInfo.IsActive {
			sessionStatus += "  " + successStyle.Render("● active")
		} else if !wt.SessionInfo.LastActivity.IsZero() {
			ago := time.Since(wt.SessionInfo.LastActivity)
			sessionStatus += "  " + dimStyle.Render(formatDuration(ago)+" ago")
		}
		lines = append(lines, sessionStatus)
	}
	if len(wt.Processes) > 0 {
		lines = append(lines, "", sectionTitle(iconProcess+" Processes"))
		for _, p := range wt.Processes {
			lines = append(lines, dimStyle.Render("  "+p))
		}
	}
	if len(wt.Commits) > 0 {
		lines = append(lines, "", sectionTitle(iconCommit+" Commits"))
		for _, c := range wt.Commits {
			lines = append(lines, dimStyle.Render(c.Hash)+" "+c.Msg)
		}
	}

	return strings.Join(lines, "\n")
}

func renderOrphanPreview(name string) string {
	lines := []string{
		sectionTitle(iconOrphan + " Orphaned Session"),
		"",
		textStyle.Render(name),
		"",
		warnStyle.Render("No matching worktree found."),
		"",
		dimStyle.Render("Actions: jump, adopt, or kill"),
	}
	return strings.Join(lines, "\n")
}

func renderRecentPreview(r recent.Entry) string {
	lines := []string{
		sectionTitle(iconPath + " Other Project"),
		"",
		sectionTitle("Project"),
		textStyle.Render(r.RepoName),
		"",
		sectionTitle("Worktree"),
		textStyle.Render(r.Worktree),
		"",
		sectionTitle("Session"),
		textStyle.Render(r.SessionName),
		"",
		sectionTitle("Path"),
		dimStyle.Render(r.Path),
		"",
		dimStyle.Render("Press enter to switch to this session"),
	}
	return strings.Join(lines, "\n")
}

func renderCreatePreview() string {
	lines := []string{
		sectionTitle(iconCreate + " Create New Worktree"),
		"",
		dimStyle.Render("This will:"),
		"",
		textStyle.Render("  1. " + iconBranch + "  Create a git branch"),
		textStyle.Render("  2. " + iconWorktree + "  Create a worktree"),
		textStyle.Render("  3. " + iconSession + "  Start a tmux session"),
		textStyle.Render("  4. " + iconJump + "  Switch to session"),
	}
	return strings.Join(lines, "\n")
}

func renderGlobalCreatePreview() string {
	lines := []string{
		sectionTitle(iconCreate + " Create New Worktree"),
		"",
		warnStyle.Render("Not available in global view"),
		"",
		dimStyle.Render("Press 'g' to switch to repo view first,"),
		dimStyle.Render("then create a new worktree."),
	}
	return strings.Join(lines, "\n")
}

func renderGlobalWorktreePreview(wt scanner.RepoWorktree) string {
	lines := []string{
		sectionTitle(iconPath + " Worktree"),
		"",
		sectionTitle("Project"),
		textStyle.Render(wt.RepoName),
		"",
		sectionTitle("Worktree"),
		textStyle.Render(wt.Worktree.Name),
		"",
		sectionTitle("Branch"),
		textStyle.Render(wt.Worktree.Branch),
		"",
		sectionTitle("Path"),
		dimStyle.Render(wt.Worktree.Path),
		"",
		dimStyle.Render("Press enter to jump to this worktree"),
	}
	return strings.Join(lines, "\n")
}

func renderMenu(title string, m *list.Model) string {
	header := titleStyle.Render("▲ " + title)
	divider := separatorStyle.Render("────────────────────────")
	return modalStyle.Render(header + "\n" + divider + "\n\n" + m.View())
}

func renderPrompt(title, label, input string) string {
	header := titleStyle.Render("▲ " + title)
	divider := separatorStyle.Render("────────────────────────")
	labelStyled := sectionStyle.Render(label)
	return modalStyle.Render(header + "\n" + divider + "\n\n" + labelStyled + "\n" + input)
}

func renderHelp() string {
	helpLine := func(key, desc string) string {
		k := lipgloss.NewStyle().Foreground(teal).Width(10).Render(key)
		d := dimStyle.Render(desc)
		return k + d
	}
	content := strings.Join([]string{
		titleStyle.Render("▲ treemux help"),
		"",
		sectionTitle("Navigation"),
		helpLine("j / ↓", "move down"),
		helpLine("k / ↑", "move up"),
		helpLine("enter", "jump to worktree / create"),
		"",
		sectionTitle("Actions"),
		helpLine("tab", "open actions menu"),
		helpLine("ctrl+d", "delete worktree + session"),
		"",
		sectionTitle("Other"),
		helpLine("?", "toggle this help"),
		helpLine("esc / q", "quit (back in dialogs)"),
	}, "\n")
	return modalStyle.Render(content)
}

func statusLine(s *git.StatusSummary) string {
	if s == nil {
		return dimStyle.Render("unknown")
	}
	if s.Clean {
		return successStyle.Render(iconClean + " Clean")
	}
	parts := []string{}
	if s.Staged > 0 {
		parts = append(parts, successStyle.Render(fmt.Sprintf("%d staged", s.Staged)))
	}
	if s.Modified > 0 {
		parts = append(parts, warnStyle.Render(fmt.Sprintf(iconModified+" %d modified", s.Modified)))
	}
	if s.Untracked > 0 {
		parts = append(parts, dimStyle.Render(fmt.Sprintf("%d untracked", s.Untracked)))
	}
	return strings.Join(parts, "  ")
}

type key struct {
	k string
	d string
}

func actionMenuItems(hasSession bool) []list.Item {
	items := []list.Item{
		listItem{title: iconJump + "  Jump", desc: "Switch to session", kind: kindHeader},
		listItem{title: iconDelete + "  Delete worktree", desc: "Delete worktree + session", kind: kindHeader},
	}
	if hasSession {
		items = append(items, listItem{title: iconKill + "  Kill session", desc: "Kill tmux session only", kind: kindHeader})
	}
	return items
}

func orphanMenuItems() []list.Item {
	return []list.Item{
		listItem{title: iconJump + "  Jump", desc: "Switch to session", kind: kindHeader},
		listItem{title: iconAdopt + "  Adopt", desc: "Create worktree for this session", kind: kindHeader},
		listItem{title: iconKill + "  Kill session", desc: "Kill orphaned session", kind: kindHeader},
	}
}

func globalActionMenuItems() []list.Item {
	return []list.Item{
		listItem{title: iconJump + "  Jump", desc: "Switch to session", kind: kindHeader},
	}
}

func globalOrphanMenuItems() []list.Item {
	return []list.Item{
		listItem{title: iconJump + "  Jump", desc: "Switch to session", kind: kindHeader},
		listItem{title: iconKill + "  Kill session", desc: "Kill orphaned session", kind: kindHeader},
	}
}

func filterStrings(in []string, omit string) []string {
	out := []string{}
	for _, s := range in {
		if s != omit {
			out = append(out, s)
		}
	}
	return out
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// Styling - Catppuccin Mocha

var (
	baseBg       = lipgloss.Color("#11111b")
	panelBg      = lipgloss.Color("#1e1e2e")
	surfaceBg    = lipgloss.Color("#313244")
	accent       = lipgloss.Color("#cba6f7")
	accent2      = lipgloss.Color("#89b4fa")
	teal         = lipgloss.Color("#94e2d5")
	successColor = lipgloss.Color("#a6e3a1")
	warnColor    = lipgloss.Color("#f9e2af")
	errorColor   = lipgloss.Color("#f38ba8")
	textColor    = lipgloss.Color("#cdd6f4")
	subTextColor = lipgloss.Color("#a6adc8")
	dimColor     = lipgloss.Color("#6c7086")
	overlayColor = lipgloss.Color("#45475a")
)

const (
	iconWorktree   = ""
	iconCurrent    = ""
	iconOrphan     = ""
	iconCreate     = ""
	iconBranch     = ""
	iconSession    = ""
	iconClean      = ""
	iconModified   = ""
	iconAhead      = ""
	iconBehind     = ""
	iconProcess    = ""
	iconCommit     = ""
	iconPath       = ""
	iconJump       = ""
	iconDelete     = ""
	iconKill       = ""
	iconAdopt      = ""
)

var (
	titleStyle = lipgloss.NewStyle().
			Foreground(accent).
			Bold(true)
	sectionStyle = lipgloss.NewStyle().
			Foreground(accent2).
			Bold(true)
	textStyle = lipgloss.NewStyle().
			Foreground(textColor)
	subTextStyle = lipgloss.NewStyle().
			Foreground(subTextColor)
	dimStyle = lipgloss.NewStyle().
			Foreground(dimColor)
	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true)
	successStyle = lipgloss.NewStyle().
			Foreground(successColor)
	warnStyle = lipgloss.NewStyle().
			Foreground(warnColor)
	listFrameStyle = lipgloss.NewStyle().
			Padding(1, 2)
	previewFrameStyle = lipgloss.NewStyle().
				Padding(1, 2).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(overlayColor)
	modalStyle = lipgloss.NewStyle().
			Padding(1, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent)
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accent).
			Padding(0, 2)
	footerStyle = lipgloss.NewStyle().
			Foreground(subTextColor).
			Padding(0, 2)
	keyStyle = lipgloss.NewStyle().
			Foreground(teal).
			Bold(true)
	separatorStyle = lipgloss.NewStyle().
			Foreground(overlayColor)
	orphanHeaderStyle = lipgloss.NewStyle().
				Foreground(warnColor).
				Bold(true)
	currentStyle = lipgloss.NewStyle().
			Foreground(accent).
			Bold(true)
	branchStyle = lipgloss.NewStyle().
			Foreground(subTextColor)
	selectedStyle = lipgloss.NewStyle().
			Background(surfaceBg).
			Foreground(accent).
			Bold(true)
	selectedBranchStyle = lipgloss.NewStyle().
				Background(surfaceBg).
				Foreground(teal)
	selectedOrphanStyle = lipgloss.NewStyle().
				Background(surfaceBg).
				Foreground(warnColor).
				Bold(true)
)

func sectionTitle(s string) string {
	title := sectionStyle.Render(s)
	return title
}

type itemDelegate struct {
	listWidth int
}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	i, ok := item.(listItem)
	if !ok {
		return
	}

	selected := index == m.Index()
	width := d.listWidth - 4

	var line string
	switch i.kind {
	case kindCreate:
		text := "+ Create new worktree ..."
		padded := fmt.Sprintf("%-*s", width, text)
		if selected {
			line = selectedStyle.Render(padded)
		} else {
			line = textStyle.Render(text)
		}
	case kindWorktree:
		wt := i.data.(workspace.WorktreeState)
		name := wt.Worktree.Name
		branch := wt.Worktree.Branch

		statusDot := successStyle.Render("●")
		if wt.Status != nil && !wt.Status.Clean {
			if wt.Status.Staged > 0 {
				statusDot = sectionStyle.Render("●")
			} else {
				statusDot = warnStyle.Render("●")
			}
		}

		sessionIcon := ""
		if wt.HasSession {
			if wt.SessionInfo != nil && wt.SessionInfo.IsActive {
				sessionIcon = " " + successStyle.Render(iconSession)
			} else {
				sessionIcon = " " + dimStyle.Render(iconSession)
			}
		}

		suffix := branch + sessionIcon
		nameWidth := width - len(branch) - 6
		if nameWidth < 10 {
			nameWidth = 10
		}

		indicator := "  "
		if i.isCurrent {
			indicator = iconCurrent + " "
		}

		paddedName := fmt.Sprintf("%-*s", nameWidth, indicator+name)
		if selected {
			fullLine := statusDot + " " + paddedName + "  " + suffix
			padded := fmt.Sprintf("%-*s", width, fullLine)
			line = selectedStyle.Render(padded)
		} else {
			line = statusDot + " " + textStyle.Render(paddedName) + "  " + branchStyle.Render(suffix)
		}
	case kindSeparator:
		line = separatorStyle.Render(strings.Repeat("─", width))
	case kindHeader:
		suffix := ""
		if i.title == "ORPHANED SESSIONS" {
			suffix = " " + dimStyle.Render("(no worktree)")
		} else if i.title == "RECENT" {
			suffix = " " + dimStyle.Render("(other projects)")
		}
		line = orphanHeaderStyle.Render(i.title + suffix)
	case kindOrphan:
		name := i.title
		label := "orphan"
		nameWidth := width - len(label) - 5
		if nameWidth < 10 {
			nameWidth = 10
		}
		paddedName := fmt.Sprintf("%-*s", nameWidth, name)
		if selected {
			fullLine := "   " + paddedName + "  " + label
			padded := fmt.Sprintf("%-*s", width, fullLine)
			line = selectedOrphanStyle.Render(padded)
		} else {
			line = "   " + warnStyle.Render(paddedName) + "  " + dimStyle.Render(label)
		}
	case kindRecent:
		name := i.title
		label := "other project"
		nameWidth := width - len(label) - 5
		if nameWidth < 10 {
			nameWidth = 10
		}
		paddedName := fmt.Sprintf("%-*s", nameWidth, name)
		if selected {
			fullLine := "   " + paddedName + "  " + label
			padded := fmt.Sprintf("%-*s", width, fullLine)
			line = selectedStyle.Render(padded)
		} else {
			line = "   " + sectionStyle.Render(paddedName) + "  " + dimStyle.Render(label)
		}
	case kindRepoHeader:
		line = sectionStyle.Render(i.title)
	case kindGlobal:
		wt := i.data.(scanner.RepoWorktree)
		name := wt.Worktree.Name
		branch := wt.Worktree.Branch
		nameWidth := width - len(branch) - 6
		if nameWidth < 10 {
			nameWidth = 10
		}
		paddedName := fmt.Sprintf("   %-*s", nameWidth, name)
		if selected {
			fullLine := paddedName + "  " + branch
			padded := fmt.Sprintf("%-*s", width, fullLine)
			line = selectedStyle.Render(padded)
		} else {
			line = textStyle.Render(paddedName) + "  " + branchStyle.Render(branch)
		}
	}

	fmt.Fprint(w, line)
}

func newItemDelegate(width int) itemDelegate {
	return itemDelegate{listWidth: width}
}
