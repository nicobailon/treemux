package tui

import (
	"fmt"
	"io"
	"path/filepath"
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
	"github.com/nicobailon/treemux/internal/tui/components"
	"github.com/nicobailon/treemux/internal/tui/theme"
	"github.com/nicobailon/treemux/internal/tui/views"
	"github.com/nicobailon/treemux/internal/workspace"
)

type viewState int

const (
	stateMain viewState = iota
	stateSelectRepo
	stateCreateName
	stateCreateBranch
	stateOrphanBranch
	stateActionMenu
	stateOrphanMenu
	stateHelp
	stateCommandPalette
	stateGridView
	stateGridDetail
)

const defaultRefreshInterval = 3 * time.Second

type Deps struct {
	Svc         *workspace.Service
	Cfg         *config.Config
	Tmux        *tmux.Tmux
	RecentStore *recent.Store
}

type WorkspaceData struct {
	States          []workspace.WorktreeState
	Orphans         []string
	RecentEntries   []recent.Entry
	GlobalWorktrees []scanner.RepoWorktree
	AvailableRepos  []views.RepoInfo
}

type PendingAction struct {
	Name        string
	Worktree    *workspace.WorktreeState
	Global      *scanner.RepoWorktree
	CreateSvc   *workspace.Service
	SelectAfter string
}

type Navigation struct {
	State           viewState
	PrevState       viewState
	NextBranchState viewState
	GlobalMode      bool
	InGitRepo       bool
	Loading         bool
}

type itemKind int

const (
	kindCreate itemKind = iota
	kindGridView
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

type CommandItem struct {
	label string
	desc  string
	run   func(*model) tea.Cmd
}

func (c CommandItem) Title() string       { return c.label }
func (c CommandItem) Description() string { return c.desc }
func (c CommandItem) FilterValue() string { return c.label + " " + c.desc }

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

type refreshTickMsg struct{}
type previewTickMsg struct{}
type paneContentMsg struct {
	sessionName string
	content     string
	err         error
}

const previewRefreshInterval = 500 * time.Millisecond

type model struct {
	deps            Deps
	data            WorkspaceData
	pending         PendingAction
	nav             Navigation
	list            list.Model
	preview         viewport.Model
	input           textinput.Model
	menu            list.Model
	spinner         spinner.Model
	commandPalette  list.Model
	width           int
	height          int
	toast           *toast
	jumpTarget       *JumpTarget
	refreshInterval  time.Duration
	refreshInFlight  int
	paneContent string
	paneSession string
	grid        views.GridState
}

type JumpTarget struct {
	SessionName string
	Path        string
	Create      bool
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
	l.FilterInput.PromptStyle = theme.KeyStyle
	l.FilterInput.TextStyle = theme.TextStyle

	ti := textinput.New()
	ti.CharLimit = 64
	ti.Placeholder = "worktree-name"
	ti.Prompt = ""
	ti.TextStyle = theme.TextStyle
	ti.PlaceholderStyle = theme.SubTextStyle

	menuDel := list.NewDefaultDelegate()
	menuDel.ShowDescription = true
	menuDel.Styles.NormalTitle = theme.TextStyle
	menuDel.Styles.NormalDesc = theme.DimStyle
	menuDel.Styles.SelectedTitle = theme.CurrentStyle
	menuDel.Styles.SelectedDesc = theme.SectionStyle
	menu := list.New([]list.Item{}, menuDel, 0, 0)
	menu.DisableQuitKeybindings()
	menu.SetShowHelp(false)
	menu.SetShowStatusBar(false)
	menu.SetFilteringEnabled(false)
	menu.SetShowTitle(false)
	menu.SetShowPagination(false)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(theme.Accent)

	vp := viewport.New(0, 0)

	recentStore, _ := recent.Load()

	cmdPaletteDel := list.NewDefaultDelegate()
	cmdPaletteDel.ShowDescription = true
	cmdPaletteDel.Styles.NormalTitle = theme.TextStyle
	cmdPaletteDel.Styles.NormalDesc = theme.DimStyle
	cmdPaletteDel.Styles.SelectedTitle = theme.CurrentStyle
	cmdPaletteDel.Styles.SelectedDesc = theme.SectionStyle
	cmdPalette := list.New([]list.Item{}, cmdPaletteDel, 0, 0)
	cmdPalette.DisableQuitKeybindings()
	cmdPalette.SetShowHelp(false)
	cmdPalette.SetShowStatusBar(false)
	cmdPalette.SetFilteringEnabled(true)
	cmdPalette.SetShowTitle(false)
	cmdPalette.SetShowPagination(false)
	cmdPalette.FilterInput.Prompt = "> "
	cmdPalette.FilterInput.PromptStyle = theme.KeyStyle
	cmdPalette.FilterInput.TextStyle = theme.TextStyle

	return model{
		deps: Deps{
			Svc:         svc,
			Cfg:         cfg,
			Tmux:        t,
			RecentStore: recentStore,
		},
		nav: Navigation{
			State:           stateGridView,
			NextBranchState: stateCreateBranch,
			Loading:         true,
			GlobalMode:      !inGitRepo,
			InGitRepo:       inGitRepo,
		},
		list:            l,
		preview:         vp,
		input:           ti,
		menu:            menu,
		commandPalette:  cmdPalette,
		spinner:         sp,
		refreshInterval: defaultRefreshInterval,
	}
}

// TEA plumbing

func (m model) Init() tea.Cmd {
	if m.nav.GlobalMode {
		return tea.Batch(m.spinner.Tick, loadGlobalDataCmd(m.deps.Cfg, m.deps.Tmux), m.tickCmd(), m.previewTickCmd())
	}
	return tea.Batch(m.spinner.Tick, loadDataCmd(m.deps.Svc), m.tickCmd(), m.previewTickCmd())
}

func (m model) tickCmd() tea.Cmd {
	return tea.Tick(m.refreshInterval, func(time.Time) tea.Msg {
		return refreshTickMsg{}
	})
}

func (m model) previewTickCmd() tea.Cmd {
	return tea.Tick(previewRefreshInterval, func(time.Time) tea.Msg {
		return previewTickMsg{}
	})
}

func loadPaneContentCmd(t *tmux.Tmux, sessionName string, lines int) tea.Cmd {
	return func() tea.Msg {
		content, err := t.CapturePane(sessionName, lines)
		return paneContentMsg{sessionName: sessionName, content: content, err: err}
	}
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
		def := svc.Git.DefaultBranch()
		sort.Strings(branches)
		if def != "" {
			branches = append([]string{def}, views.FilterStrings(branches, def)...)
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
		m.nav.Loading = false
		if m.refreshInFlight > 0 {
			m.refreshInFlight--
		}
		repoRoot := ""
		if m.deps.Svc != nil && m.deps.Svc.Git != nil {
			repoRoot = m.deps.Svc.Git.RepoRoot
		}
		m.data.States = reorderCurrentFirst(msg.states, repoRoot)
		m.data.Orphans = msg.orphans
		if m.deps.RecentStore != nil && repoRoot != "" {
			m.data.RecentEntries = m.deps.RecentStore.GetOtherProjects(repoRoot, 5)
		}
		items := buildItems(m.data.States, m.data.Orphans, m.data.RecentEntries, repoRoot)
		m.list.SetItems(items)
		if m.pending.SelectAfter != "" {
			for i, item := range items {
				if li, ok := item.(listItem); ok && li.kind == kindWorktree && li.title == m.pending.SelectAfter {
					m.list.Select(i)
					break
				}
			}
			m.pending.SelectAfter = ""
		}
		
		if m.nav.State == stateGridView {
			wasInAvailable := m.grid.InAvailable
			var prevName string
			if wasInAvailable {
				filtered := m.grid.FilteredAvailable()
				if m.grid.AvailIdx < len(filtered) {
					prevName = filtered[m.grid.AvailIdx].Name
				}
			} else if m.grid.Index >= 0 {
				filtered := m.grid.FilteredPanels()
				if m.grid.Index < len(filtered) {
					prevName = filtered[m.grid.Index].Name
				}
			}
			m.buildGridPanels()
			if wasInAvailable {
				filteredAvail := m.grid.FilteredAvailable()
				m.grid.AvailIdx = 0
				for i, p := range filteredAvail {
					if p.Name == prevName {
						m.grid.AvailIdx = i
						break
					}
				}
				if len(filteredAvail) > 0 {
					m.grid.InAvailable = true
				} else {
					m.grid.InAvailable = false
				}
			} else if m.grid.Index >= 0 {
				filteredPanels := m.grid.FilteredPanels()
				m.grid.Index = 0
				for i, p := range filteredPanels {
					if p.Name == prevName {
						m.grid.Index = i
						break
					}
				}
				if len(filteredPanels) == 0 && len(m.grid.FilteredAvailable()) > 0 {
					m.grid.InAvailable = true
					m.grid.AvailIdx = 0
				}
			}
			return m, m.loadGridContentCmd()
		}
		return m, nil

	case globalDataLoadedMsg:
		m.nav.Loading = false
		if m.refreshInFlight > 0 {
			m.refreshInFlight--
		}
		m.data.GlobalWorktrees = msg.worktrees
		m.data.Orphans = msg.orphans
		items := buildGlobalItems(m.data.GlobalWorktrees, m.data.Orphans, m.deps.Tmux)
		m.list.SetItems(items)
		if m.pending.SelectAfter != "" {
			for i, item := range items {
				if li, ok := item.(listItem); ok && li.kind == kindGlobal && li.title == m.pending.SelectAfter {
					m.list.Select(i)
					break
				}
			}
			m.pending.SelectAfter = ""
		}
		
		if m.nav.State == stateGridView {
			wasInAvailable := m.grid.InAvailable
			var prevName string
			if wasInAvailable {
				filtered := m.grid.FilteredAvailable()
				if m.grid.AvailIdx < len(filtered) {
					prevName = filtered[m.grid.AvailIdx].Name
				}
			} else if m.grid.Index >= 0 {
				filtered := m.grid.FilteredPanels()
				if m.grid.Index < len(filtered) {
					prevName = filtered[m.grid.Index].Name
				}
			}
			m.buildGridPanels()
			if prevName == "" {
				m.grid.Index = 0
				m.grid.Filter = ""
				m.grid.Filtering = false
				m.grid.ScrollOffset = 0
				m.grid.InAvailable = false
				m.grid.AvailIdx = 0
				if len(m.grid.Panels) == 0 && len(m.grid.FilteredAvailable()) > 0 {
					m.grid.InAvailable = true
				}
			} else if wasInAvailable {
				filteredAvail := m.grid.FilteredAvailable()
				m.grid.AvailIdx = 0
				for i, p := range filteredAvail {
					if p.Name == prevName {
						m.grid.AvailIdx = i
						break
					}
				}
				if len(filteredAvail) > 0 {
					m.grid.InAvailable = true
				} else {
					m.grid.InAvailable = false
				}
			} else if m.grid.Index >= 0 {
				filteredPanels := m.grid.FilteredPanels()
				m.grid.Index = 0
				for i, p := range filteredPanels {
					if p.Name == prevName {
						m.grid.Index = i
						break
					}
				}
				if len(filteredPanels) == 0 && len(m.grid.FilteredAvailable()) > 0 {
					m.grid.InAvailable = true
					m.grid.AvailIdx = 0
				}
			}
			return m, m.loadGridContentCmd()
		}
		return m, nil

	case branchesMsg:
		items := []list.Item{}
		for _, b := range msg.branches {
			items = append(items, listItem{title: b, desc: "base branch", kind: kindWorktree})
		}
		m.menu.SetItems(items)
		selectedIdx := 0
		if m.deps.Svc != nil && m.deps.Svc.Git != nil {
			var currentBranch string
			for _, st := range m.data.States {
				if st.Worktree.Path == m.deps.Svc.Git.RepoRoot {
					currentBranch = st.Worktree.Branch
					break
				}
			}
			if currentBranch != "" {
				for i, b := range msg.branches {
					if b == currentBranch {
						selectedIdx = i
						break
					}
				}
			}
		}
		m.menu.Select(selectedIdx)
		m.nav.State = m.nav.NextBranchState
		return m, nil

	case jumpMsg:
		m.jumpTarget = &JumpTarget{SessionName: msg.sessionName, Path: msg.path}
		return m, tea.Quit

	case resultMsg:
		if msg.action == "load" {
			m.nav.Loading = false
			if m.refreshInFlight > 0 {
				m.refreshInFlight--
			}
		}
		if msg.err != nil {
			m.toast = &toast{
				message:   msg.err.Error(),
				kind:      toastError,
				expiresAt: time.Now().Add(toastDuration),
			}
			return m, toastExpireCmd()
		}
		switch msg.action {
		case "create":
			m.toast = &toast{message: "Worktree created", kind: toastSuccess, expiresAt: time.Now().Add(toastDuration)}
		case "delete":
			m.toast = &toast{message: "Worktree deleted", kind: toastSuccess, expiresAt: time.Now().Add(toastDuration)}
		case "kill-session":
			m.toast = &toast{message: "Session killed", kind: toastSuccess, expiresAt: time.Now().Add(toastDuration)}
		case "adopt":
			m.toast = &toast{message: "Session adopted", kind: toastSuccess, expiresAt: time.Now().Add(toastDuration)}
		}
		switch msg.action {
		case "create", "delete", "kill-session", "adopt":
			m.nav.State = stateMain
			m.pending.CreateSvc = nil
			if m.nav.GlobalMode {
				return m, tea.Batch(loadGlobalDataCmd(m.deps.Cfg, m.deps.Tmux), toastExpireCmd())
			}
			if m.deps.Svc == nil {
				return m, toastExpireCmd()
			}
			return m, tea.Batch(loadDataCmd(m.deps.Svc), toastExpireCmd())
		}
		return m, nil

	case refreshTickMsg:
		if m.refreshInFlight > 0 || m.nav.Loading {
			return m, m.tickCmd()
		}
		m.refreshInFlight++
		if m.nav.GlobalMode {
			return m, tea.Batch(loadGlobalDataCmd(m.deps.Cfg, m.deps.Tmux), m.tickCmd())
		}
		if m.deps.Svc == nil {
			m.refreshInFlight--
			return m, m.tickCmd()
		}
		return m, tea.Batch(loadDataCmd(m.deps.Svc), m.tickCmd())

	case previewTickMsg:
		sel, ok := m.list.SelectedItem().(listItem)
		if !ok {
			return m, m.previewTickCmd()
		}
		var sessionName string
		switch sel.kind {
		case kindWorktree:
			wt := sel.data.(workspace.WorktreeState)
			if wt.HasSession {
				sessionName = wt.SessionName
			}
		case kindGlobal:
			wt := sel.data.(scanner.RepoWorktree)
			if m.deps.Tmux.HasSession(wt.Worktree.Name) {
				sessionName = wt.Worktree.Name
			}
		case kindOrphan:
			sessionName = sel.title
		}
		if sessionName != "" {
			return m, tea.Batch(loadPaneContentCmd(m.deps.Tmux, sessionName, 50), m.previewTickCmd())
		}
		m.paneContent = ""
		m.paneSession = ""
		return m, m.previewTickCmd()

	case paneContentMsg:
		if msg.err == nil && msg.content != "" {
			sel, ok := m.list.SelectedItem().(listItem)
			if ok {
				var expectedSession string
				switch sel.kind {
				case kindWorktree:
					wt := sel.data.(workspace.WorktreeState)
					if wt.HasSession {
						expectedSession = wt.SessionName
					}
				case kindGlobal:
					wt := sel.data.(scanner.RepoWorktree)
					expectedSession = wt.Worktree.Name
				case kindOrphan:
					expectedSession = sel.title
				}
				if msg.sessionName == expectedSession {
					m.paneContent = msg.content
					m.paneSession = msg.sessionName
				}
			}
		}
		return m, nil

	case views.GridContentMsg:
		for i := range m.grid.Panels {
			if content, ok := msg.Contents[m.grid.Panels[i].SessionName]; ok {
				m.grid.Panels[i].Content = content
			}
		}
		return m, nil

	case SuccessMsg:
		m.toast = &toast{message: msg.Message, kind: toastSuccess, expiresAt: time.Now().Add(toastDuration)}
		return m, toastExpireCmd()

	case ErrorMsg:
		m.toast = &toast{message: msg.Error(), kind: toastError, expiresAt: time.Now().Add(toastDuration)}
		return m, toastExpireCmd()

	case WarningMsg:
		m.toast = &toast{message: msg.Message, kind: toastWarning, expiresAt: time.Now().Add(toastDuration)}
		return m, toastExpireCmd()

	case InfoMsg:
		m.toast = &toast{message: msg.Message, kind: toastInfo, expiresAt: time.Now().Add(toastDuration)}
		return m, toastExpireCmd()

	case toastExpiredMsg:
		if m.toast != nil && m.toast.expired() {
			m.toast = nil
		}
		return m, nil
	}

	// state-specific handling
	switch m.nav.State {
	case stateSelectRepo:
		var cmd tea.Cmd
		m.menu, cmd = m.menu.Update(msg)
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				idx := m.menu.Index()
				if idx >= 0 && idx < len(m.data.AvailableRepos) {
					repo := m.data.AvailableRepos[idx]
					g := &git.Git{RepoRoot: repo.Root}
					m.pending.CreateSvc = workspace.NewService(g, m.deps.Tmux, m.deps.Cfg)
					m.nav.State = stateCreateName
					m.input.SetValue("")
					return m, m.input.Focus()
				}
			case "esc":
				m.nav.State = stateMain
				m.pending.CreateSvc = nil
			}
		}
		return m, cmd

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
				m.pending.Name = name
				m.nav.NextBranchState = stateCreateBranch
				svc := m.deps.Svc
				if m.pending.CreateSvc != nil {
					svc = m.pending.CreateSvc
				}
				if svc == nil {
					m.nav.State = stateMain
					return m, nil
				}
				return m, branchesCmd(svc)
			case "esc":
				m.nav.State = stateMain
				m.pending.CreateSvc = nil
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
					name := m.pending.Name
					svc := m.deps.Svc
					if m.pending.CreateSvc != nil {
						svc = m.pending.CreateSvc
					}
					if svc == nil {
						m.nav.State = stateMain
						return m, nil
					}
					m.pending.SelectAfter = filepath.Base(svc.WorktreePath(name))
					m.nav.State = stateMain
					return m, createWorktreeCmd(svc, name, branch)
				}
			case "esc":
				m.nav.State = stateMain
				m.pending.CreateSvc = nil
			}
		}
		return m, cmd

	case stateOrphanBranch:
		var cmd tea.Cmd
		m.menu, cmd = m.menu.Update(msg)
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				if m.deps.Svc == nil {
					m.nav.State = stateMain
					return m, nil
				}
				if sel, ok := m.menu.SelectedItem().(listItem); ok {
					branch := sel.title
					name := m.pending.Name
					m.pending.SelectAfter = filepath.Base(m.deps.Svc.WorktreePath(name))
					m.nav.State = stateMain
					return m, adoptCmd(m.deps.Svc, name, branch)
				}
			case "esc":
				m.nav.State = stateMain
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
					if m.pending.Worktree != nil && m.deps.Svc != nil {
						sessionName := m.deps.Svc.SessionName(m.pending.Worktree.Worktree.Path)
						if !m.deps.Svc.Tmux.HasSession(sessionName) {
							_ = m.deps.Svc.Tmux.NewSession(sessionName, m.pending.Worktree.Worktree.Path)
						}
						if m.deps.RecentStore != nil && m.deps.Svc.Git != nil {
							m.deps.RecentStore.Add(m.deps.Svc.Git.RepoRoot, m.pending.Worktree.Worktree.Name, sessionName, m.pending.Worktree.Worktree.Path)
							_ = m.deps.RecentStore.Save()
						}
						m.jumpTarget = &JumpTarget{SessionName: sessionName, Path: m.pending.Worktree.Worktree.Path}
						return m, tea.Quit
					}
					if m.pending.Global != nil {
						sessionName := m.pending.Global.Worktree.Name
						if !m.deps.Tmux.HasSession(sessionName) {
							_ = m.deps.Tmux.NewSession(sessionName, m.pending.Global.Worktree.Path)
						}
						if m.deps.RecentStore != nil {
							m.deps.RecentStore.Add(m.pending.Global.RepoRoot, m.pending.Global.Worktree.Name, sessionName, m.pending.Global.Worktree.Path)
							_ = m.deps.RecentStore.Save()
						}
						m.jumpTarget = &JumpTarget{SessionName: sessionName, Path: m.pending.Global.Worktree.Path}
						return m, tea.Quit
					}
					if m.pending.Name != "" {
						m.jumpTarget = &JumpTarget{SessionName: m.pending.Name}
						return m, tea.Quit
					}
				case strings.Contains(title, "Delete worktree"):
					if m.pending.Worktree != nil && m.deps.Svc != nil && m.deps.Svc.Git != nil {
						if m.pending.Worktree.Worktree.Path == m.deps.Svc.Git.RepoRoot {
							m.toast = &toast{message: "Cannot delete current worktree", kind: toastError, expiresAt: time.Now().Add(toastDuration)}
							m.nav.State = stateMain
							return m, toastExpireCmd()
						}
						return m, deleteWorktreeCmd(m.deps.Svc, m.pending.Worktree.Worktree.Path)
					}
				case strings.Contains(title, "Kill session"):
					if m.pending.Worktree != nil && m.deps.Svc != nil {
						return m, killSessionCmd(m.deps.Svc, m.pending.Worktree.SessionName)
					}
					if m.pending.Name != "" {
						if m.nav.GlobalMode {
							return m, killSessionDirectCmd(m.deps.Tmux, m.pending.Name)
						}
						if m.deps.Svc != nil {
							return m, killSessionCmd(m.deps.Svc, m.pending.Name)
						}
					}
				case strings.Contains(title, "Adopt"):
					if m.pending.Name != "" && m.deps.Svc != nil {
						m.nav.NextBranchState = stateOrphanBranch
						return m, branchesCmd(m.deps.Svc)
					}
				}
				if m.nav.PrevState != 0 {
					m.nav.State = m.nav.PrevState
				} else {
					m.nav.State = stateMain
				}
				return m, nil
			case "esc":
				if m.nav.PrevState != 0 {
					m.nav.State = m.nav.PrevState
				} else {
					m.nav.State = stateMain
				}
				return m, nil
			}
		}
		return m, cmd

	case stateCommandPalette:
		var cmd tea.Cmd
		m.commandPalette, cmd = m.commandPalette.Update(msg)
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "esc":
				m.nav.State = stateMain
				return m, nil
			case "enter":
				if item, ok := m.commandPalette.SelectedItem().(CommandItem); ok {
					m.nav.State = stateMain
					if item.run != nil {
						return m, item.run(&m)
					}
				}
				m.nav.State = stateMain
				return m, nil
			}
		}
		return m, cmd
	}

	// main view handling
	var cmds []tea.Cmd
	var cmd tea.Cmd

	// Handle navigation to skip non-selectable items
	if keyMsg, ok := msg.(tea.KeyMsg); ok && m.nav.State != stateGridView && m.nav.State != stateGridDetail {
		switch keyMsg.String() {
		case "j", "down":
			m.list, cmd = m.list.Update(msg)
			cmds = append(cmds, cmd)
			m.skipNonSelectable(1)
			m.paneContent = ""
			m.paneSession = ""
			
			return m, tea.Batch(cmds...)
		case "k", "up":
			m.list, cmd = m.list.Update(msg)
			cmds = append(cmds, cmd)
			m.skipNonSelectable(-1)
			m.paneContent = ""
			m.paneSession = ""
			
			return m, tea.Batch(cmds...)
		case "pgdown", "ctrl+f":
			for i := 0; i < 10; i++ {
				m.list, _ = m.list.Update(tea.KeyMsg{Type: tea.KeyDown})
			}
			m.skipNonSelectable(1)
			m.paneContent = ""
			m.paneSession = ""
			return m, nil
		case "pgup", "ctrl+b":
			for i := 0; i < 10; i++ {
				m.list, _ = m.list.Update(tea.KeyMsg{Type: tea.KeyUp})
			}
			m.skipNonSelectable(-1)
			m.paneContent = ""
			m.paneSession = ""
			return m, nil
		}
	}

	if m.nav.State != stateGridView && m.nav.State != stateGridDetail {
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.nav.State == stateGridView && m.grid.Filtering {
				m.grid.Filter += "q"
				m.grid.Index = 0
				return m, nil
			}
			return m, tea.Quit
		case "esc":
			if m.nav.State == stateMain {
				return m, tea.Quit
			}
			if m.nav.State == stateGridDetail {
				m.nav.State = stateGridView
				m.grid.DetailPanel = nil
				return m, nil
			}
			if m.nav.State == stateGridView {
				if m.grid.Filtering {
					m.grid.Filter = ""
					m.grid.Filtering = false
					m.grid.Index = 0
					m.grid.ScrollOffset = 0
					return m, nil
				}
				m.nav.State = stateMain
				return m, nil
			}
		case "?":
			if m.nav.State == stateHelp {
				m.nav.State = stateMain
			} else {
				m.nav.State = stateHelp
			}
		case "g":
			if m.nav.State == stateGridView && m.grid.Filtering {
				m.grid.Filter += "g"
				m.grid.Index = 0
				return m, nil
			}
			if m.nav.State == stateMain {
				m.nav.GlobalMode = !m.nav.GlobalMode
				m.nav.Loading = true
				if m.nav.GlobalMode {
					return m, tea.Batch(m.spinner.Tick, loadGlobalDataCmd(m.deps.Cfg, m.deps.Tmux))
				}
				if m.nav.InGitRepo {
					return m, tea.Batch(m.spinner.Tick, loadDataCmd(m.deps.Svc))
				}
				m.nav.GlobalMode = true
				return m, tea.Batch(m.spinner.Tick, loadGlobalDataCmd(m.deps.Cfg, m.deps.Tmux))
			}
		case "enter":
			if m.nav.State == stateGridView {
				if m.grid.Index == -1 {
					if m.nav.GlobalMode {
						repos := views.ExtractUniqueRepos(m.data.GlobalWorktrees)
						if len(repos) == 0 {
							m.toast = &toast{message: "No repositories found", kind: toastError, expiresAt: time.Now().Add(toastDuration)}
							return m, toastExpireCmd()
						}
						m.data.AvailableRepos = repos
						items := make([]list.Item, len(repos))
						for i, r := range repos {
							items[i] = listItem{title: r.Name, desc: r.Root, kind: kindHeader}
						}
						m.menu.SetItems(items)
						m.menu.Select(0)
						m.nav.State = stateSelectRepo
						return m, nil
					}
					m.nav.State = stateCreateName
					m.input.SetValue("")
					return m, m.input.Focus()
				}
				if m.grid.Index == -2 {
					m.nav.State = stateMain
					return m, nil
				}
				var panel *views.GridPanel
				if m.grid.InAvailable {
					filteredAvail := m.grid.FilteredAvailable()
					if m.grid.AvailIdx < len(filteredAvail) {
						p := filteredAvail[m.grid.AvailIdx]
						panel = &p
					}
				} else {
					filteredPanels := m.grid.FilteredPanels()
					if m.grid.Index >= 0 && m.grid.Index < len(filteredPanels) {
						p := filteredPanels[m.grid.Index]
						panel = &p
					}
				}
				if panel != nil {
					m.grid.DetailPanel = panel
					m.grid.DetailIdx = 0
					m.nav.State = stateGridDetail
				}
				return m, nil
			}
			if m.nav.State == stateGridDetail && m.grid.DetailPanel != nil {
				panel := m.grid.DetailPanel
				backIdx := 1
				if panel.HasSession {
					backIdx = 2
				}
				if panel.IsOrphan {
					backIdx = 3
				}
				if m.grid.DetailIdx == backIdx {
					m.nav.State = stateGridView
					m.grid.DetailPanel = nil
					return m, nil
				}
				switch m.grid.DetailIdx {
				case 0:
					if panel.HasSession {
						m.jumpTarget = &JumpTarget{SessionName: panel.SessionName, Path: panel.Path}
					} else {
						sessionName := panel.SessionName
						if sessionName == "" {
							sessionName = filepath.Base(panel.Name)
						}
						m.jumpTarget = &JumpTarget{SessionName: sessionName, Path: panel.Path, Create: true}
					}
					return m, tea.Quit
				case 1:
					if panel.IsOrphan {
						m.pending.Name = panel.SessionName
						m.nav.PrevState = stateGridView
						m.nav.NextBranchState = stateOrphanBranch
						return m, branchesCmd(m.deps.Svc)
					} else if panel.HasSession {
						return m, killSessionCmd(m.deps.Svc, panel.SessionName)
					}
				case 2:
					if panel.IsOrphan {
						return m, killSessionDirectCmd(m.deps.Tmux, panel.SessionName)
					}
				}
				return m, nil
			}
			if sel, ok := m.list.SelectedItem().(listItem); ok {
				switch sel.kind {
				case kindCreate:
					if m.nav.GlobalMode {
						repos := views.ExtractUniqueRepos(m.data.GlobalWorktrees)
						if len(repos) == 0 {
							m.toast = &toast{message: "No repositories found in search paths", kind: toastError, expiresAt: time.Now().Add(toastDuration)}
							return m, toastExpireCmd()
						}
						m.data.AvailableRepos = repos
						items := make([]list.Item, len(repos))
						for i, r := range repos {
							items[i] = listItem{title: r.Name, desc: r.Root, kind: kindHeader}
						}
						m.menu.SetItems(items)
						m.menu.Select(0)
						m.nav.State = stateSelectRepo
						return m, nil
					}
					m.nav.State = stateCreateName
					m.input.SetValue("")
					return m, m.input.Focus()
				case kindGridView:
					if !m.nav.GlobalMode {
						m.nav.GlobalMode = true
						m.nav.Loading = true
						m.nav.State = stateGridView
						return m, tea.Batch(m.spinner.Tick, loadGlobalDataCmd(m.deps.Cfg, m.deps.Tmux))
					}
					m.buildGridPanels()
					if len(m.grid.Panels) == 0 && len(m.grid.FilteredAvailable()) == 0 {
						m.toast = &toast{message: "No sessions or worktrees", kind: toastWarning, expiresAt: time.Now().Add(toastDuration)}
						return m, toastExpireCmd()
					}
					m.nav.State = stateGridView
					m.grid.Index = 0
					m.grid.Filter = ""
					m.grid.Filtering = false
					m.grid.ScrollOffset = 0
					if len(m.grid.Panels) == 0 && len(m.grid.FilteredAvailable()) > 0 {
						m.grid.InAvailable = true
						m.grid.AvailIdx = 0
					}
					return m, m.loadGridContentCmd()
				case kindWorktree:
					wt := sel.data.(workspace.WorktreeState)
					m.pending.Worktree = &wt
					m.menu.SetItems(actionMenuItems(wt.HasSession))
					m.menu.Select(0)
					m.nav.State = stateActionMenu
				case kindOrphan:
					m.pending.Name = sel.title
					m.nav.PrevState = stateMain
					if m.nav.GlobalMode {
						m.menu.SetItems(globalOrphanMenuItems())
					} else {
						m.menu.SetItems(orphanMenuItems())
					}
					m.menu.Select(0)
					m.nav.State = stateOrphanMenu
				case kindRecent:
					r := sel.data.(recent.Entry)
					if !m.deps.Tmux.HasSession(r.SessionName) {
						if err := m.deps.Tmux.NewSession(r.SessionName, r.Path); err != nil {
							m.toast = &toast{message: "Failed to create session: " + err.Error(), kind: toastError, expiresAt: time.Now().Add(toastDuration)}
							return m, toastExpireCmd()
						}
					}
					if m.deps.RecentStore != nil {
						m.deps.RecentStore.Add(r.RepoRoot, r.Worktree, r.SessionName, r.Path)
						_ = m.deps.RecentStore.Save()
					}
					m.jumpTarget = &JumpTarget{SessionName: r.SessionName, Path: r.Path}
					return m, tea.Quit
				case kindGlobal:
					wt := sel.data.(scanner.RepoWorktree)
					m.pending.Global = &wt
					m.menu.SetItems(globalActionMenuItems())
					m.menu.Select(0)
					m.nav.State = stateActionMenu
				}
			}
		case "tab":
			if m.nav.State == stateGridDetail && m.grid.DetailPanel != nil {
				maxIdx := 1
				if m.grid.DetailPanel.HasSession {
					maxIdx = 2
				}
				if m.grid.DetailPanel.IsOrphan {
					maxIdx = 3
				}
				m.grid.DetailIdx++
				if m.grid.DetailIdx > maxIdx {
					m.grid.DetailIdx = 0
				}
				return m, nil
			}
			if m.nav.State == stateGridView {
				filteredLen := len(m.grid.FilteredPanels())
				if m.grid.Index == -1 {
					m.grid.Index = -2
					m.grid.UpdateScroll(m.width, m.height)
					return m, nil
				}
				if m.grid.Index == -2 {
					if filteredLen > 0 {
						m.grid.Index = 0
					} else if len(m.grid.FilteredAvailable()) > 0 {
						m.grid.InAvailable = true
						m.grid.AvailIdx = 0
					}
					m.grid.UpdateScroll(m.width, m.height)
					return m, nil
				}
				if m.grid.InAvailable {
					if m.grid.AvailIdx < len(m.grid.FilteredAvailable())-1 {
						m.grid.AvailIdx++
					} else {
						m.grid.InAvailable = false
						m.grid.Index = -1
					}
				} else {
					if m.grid.Index < filteredLen-1 {
						m.grid.Index++
					} else if len(m.grid.FilteredAvailable()) > 0 {
						m.grid.InAvailable = true
						m.grid.AvailIdx = 0
					} else {
						m.grid.Index = -1
					}
				}
				m.grid.UpdateScroll(m.width, m.height)
				return m, nil
			}
			if sel, ok := m.list.SelectedItem().(listItem); ok && sel.kind == kindWorktree {
				wt := sel.data.(workspace.WorktreeState)
				m.pending.Worktree = &wt
				m.menu.SetItems(actionMenuItems(wt.HasSession))
				m.menu.Select(0)
				m.nav.State = stateActionMenu
			}
		case "shift+tab":
			if m.nav.State == stateGridView {
				filteredLen := len(m.grid.FilteredPanels())
				if m.grid.Index == -1 {
					if len(m.grid.FilteredAvailable()) > 0 {
						m.grid.InAvailable = true
						m.grid.AvailIdx = len(m.grid.FilteredAvailable()) - 1
					} else if filteredLen > 0 {
						m.grid.Index = filteredLen - 1
					} else {
						m.grid.Index = -2
					}
					m.grid.UpdateScroll(m.width, m.height)
					return m, nil
				}
				if m.grid.Index == -2 {
					m.grid.Index = -1
					m.grid.UpdateScroll(m.width, m.height)
					return m, nil
				}
				if m.grid.InAvailable {
					if m.grid.AvailIdx > 0 {
						m.grid.AvailIdx--
					} else if filteredLen > 0 {
						m.grid.InAvailable = false
						m.grid.Index = filteredLen - 1
					} else {
						m.grid.InAvailable = false
						m.grid.Index = -2
					}
					m.grid.UpdateScroll(m.width, m.height)
					return m, nil
				}
				if m.grid.Index == 0 {
					m.grid.Index = -2
					m.grid.UpdateScroll(m.width, m.height)
					return m, nil
				}
				if m.grid.Index > 0 {
					m.grid.Index--
				}
				m.grid.UpdateScroll(m.width, m.height)
				return m, nil
			}
		case "ctrl+d":
			if m.nav.GlobalMode || m.deps.Svc == nil {
				return m, nil
			}
			if sel, ok := m.list.SelectedItem().(listItem); ok && sel.kind == kindWorktree {
				wt := sel.data.(workspace.WorktreeState)
				if m.deps.Svc.Git != nil && wt.Worktree.Path == m.deps.Svc.Git.RepoRoot {
					m.toast = &toast{message: "Cannot delete current worktree", kind: toastError, expiresAt: time.Now().Add(toastDuration)}
					return m, toastExpireCmd()
				}
				cmds = append(cmds, deleteWorktreeCmd(m.deps.Svc, wt.Worktree.Path))
			}
		case "ctrl+p":
			return m, m.openCommandPalette()
		case "/":
			if m.nav.State == stateGridView && !m.grid.Filtering {
				m.grid.Filtering = true
				m.grid.Filter = ""
				m.grid.InAvailable = false
				m.grid.Index = 0
				return m, nil
			}
		case "ctrl+g":
			if m.nav.State == stateMain {
				if !m.nav.GlobalMode {
					m.nav.GlobalMode = true
					m.nav.Loading = true
					m.nav.State = stateGridView
					return m, tea.Batch(m.spinner.Tick, loadGlobalDataCmd(m.deps.Cfg, m.deps.Tmux))
				}
				m.buildGridPanels()
				if len(m.grid.Panels) == 0 && len(m.grid.FilteredAvailable()) == 0 {
					m.toast = &toast{message: "No sessions or worktrees", kind: toastWarning, expiresAt: time.Now().Add(toastDuration)}
					return m, toastExpireCmd()
				}
				m.nav.State = stateGridView
				m.grid.Index = 0
				m.grid.Filter = ""
				m.grid.Filtering = false
				m.grid.ScrollOffset = 0
				if len(m.grid.Panels) == 0 && len(m.grid.FilteredAvailable()) > 0 {
					m.grid.InAvailable = true
					m.grid.AvailIdx = 0
				}
				return m, m.loadGridContentCmd()
			} else if m.nav.State == stateGridView {
				m.nav.State = stateMain
				return m, nil
			}
		case "left":
			if m.nav.State == stateGridView {
				if m.grid.Index == -1 {
					return m, nil
				}
				if m.grid.Index == -2 {
					m.grid.Index = -1
					m.grid.UpdateScroll(m.width, m.height)
					return m, nil
				}
				if m.grid.InAvailable {
					if m.grid.AvailIdx > 0 {
						m.grid.AvailIdx--
					} else {
						filteredLen := len(m.grid.FilteredPanels())
						if filteredLen > 0 {
							m.grid.InAvailable = false
							m.grid.Index = filteredLen - 1
						} else {
							m.grid.InAvailable = false
							m.grid.Index = -2
						}
					}
					m.grid.UpdateScroll(m.width, m.height)
					return m, nil
				}
				if m.grid.Index == 0 {
					m.grid.Index = -2
					m.grid.UpdateScroll(m.width, m.height)
					return m, nil
				}
				if m.grid.Index > 0 {
					m.grid.Index--
				}
				m.grid.UpdateScroll(m.width, m.height)
				return m, nil
			}
		case "right":
			if m.nav.State == stateGridView {
				if m.grid.Index == -1 {
					m.grid.Index = -2
					m.grid.UpdateScroll(m.width, m.height)
					return m, nil
				}
				if m.grid.Index == -2 {
					filteredLen := len(m.grid.FilteredPanels())
					if filteredLen > 0 {
						m.grid.Index = 0
					} else if len(m.grid.FilteredAvailable()) > 0 {
						m.grid.InAvailable = true
						m.grid.AvailIdx = 0
					}
					m.grid.UpdateScroll(m.width, m.height)
					return m, nil
				}
				if m.grid.InAvailable {
					if m.grid.AvailIdx < len(m.grid.FilteredAvailable())-1 {
						m.grid.AvailIdx++
					}
				} else {
					filteredLen := len(m.grid.FilteredPanels())
					if m.grid.Index < filteredLen-1 {
						m.grid.Index++
					} else if len(m.grid.FilteredAvailable()) > 0 {
						m.grid.InAvailable = true
						m.grid.AvailIdx = 0
					}
				}
				m.grid.UpdateScroll(m.width, m.height)
				return m, nil
			}
		case "up":
			if m.nav.State == stateGridDetail {
				if m.grid.DetailIdx > 0 {
					m.grid.DetailIdx--
				}
				return m, nil
			}
			if m.nav.State == stateGridView {
				if m.grid.InAvailable {
					if m.grid.AvailIdx >= m.grid.Cols {
						m.grid.AvailIdx -= m.grid.Cols
					} else {
						filteredPanels := m.grid.FilteredPanels()
						if len(filteredPanels) > 0 {
							m.grid.InAvailable = false
							col := m.grid.AvailIdx % m.grid.Cols
							lastRow := (len(filteredPanels) - 1) / m.grid.Cols
							targetIdx := lastRow*m.grid.Cols + col
							if targetIdx >= len(filteredPanels) {
								targetIdx = len(filteredPanels) - 1
							}
							m.grid.Index = targetIdx
						} else {
							m.grid.InAvailable = false
							m.grid.Index = -2
						}
					}
				} else if m.grid.Index == -1 {
					return m, nil
				} else if m.grid.Index == -2 {
					m.grid.Index = -1
				} else {
					filteredPanels := m.grid.FilteredPanels()
					if m.grid.Index >= len(filteredPanels) || len(filteredPanels) == 0 {
						m.grid.Index = -2
						m.grid.UpdateScroll(m.width, m.height)
						return m, nil
					}
					var sessionSectionCount int
					for _, p := range filteredPanels {
						if !p.IsRecent && (p.HasSession || p.IsOrphan) {
							sessionSectionCount++
						}
					}
					currentPanel := filteredPanels[m.grid.Index]
					if currentPanel.IsRecent {
						localIdx := m.grid.Index - sessionSectionCount
						col := localIdx % m.grid.Cols
						if localIdx >= m.grid.Cols {
							m.grid.Index -= m.grid.Cols
						} else if sessionSectionCount > 0 {
							lastSessionRow := (sessionSectionCount - 1) / m.grid.Cols
							targetIdx := lastSessionRow*m.grid.Cols + col
							if targetIdx >= sessionSectionCount {
								targetIdx = sessionSectionCount - 1
							}
							m.grid.Index = targetIdx
						} else {
							m.grid.Index = -2
						}
					} else {
						if m.grid.Index >= m.grid.Cols {
							m.grid.Index -= m.grid.Cols
						} else {
							m.grid.Index = -2
						}
					}
				}
				m.grid.UpdateScroll(m.width, m.height)
				return m, nil
			}
		case "down":
			if m.nav.State == stateGridDetail && m.grid.DetailPanel != nil {
				maxIdx := 1
				if m.grid.DetailPanel.HasSession {
					maxIdx = 2
				}
				if m.grid.DetailPanel.IsOrphan {
					maxIdx = 3
				}
				if m.grid.DetailIdx < maxIdx {
					m.grid.DetailIdx++
				}
				return m, nil
			}
			if m.nav.State == stateGridView {
				if m.grid.InAvailable {
					if m.grid.AvailIdx+m.grid.Cols < len(m.grid.FilteredAvailable()) {
						m.grid.AvailIdx += m.grid.Cols
					} else {
						nextRowStart := ((m.grid.AvailIdx / m.grid.Cols) + 1) * m.grid.Cols
						if nextRowStart < len(m.grid.FilteredAvailable()) {
							m.grid.AvailIdx = nextRowStart
						}
					}
					m.grid.UpdateScroll(m.width, m.height)
					return m, nil
				}
				if m.grid.Index == -1 {
					m.grid.Index = -2
					m.grid.UpdateScroll(m.width, m.height)
					return m, nil
				}
				if m.grid.Index == -2 {
					filteredPanels := m.grid.FilteredPanels()
					if len(filteredPanels) > 0 {
						m.grid.Index = 0
					} else if len(m.grid.FilteredAvailable()) > 0 {
						m.grid.InAvailable = true
						m.grid.AvailIdx = 0
					}
					m.grid.UpdateScroll(m.width, m.height)
					return m, nil
				}
				filteredPanels := m.grid.FilteredPanels()
				if m.grid.Index >= len(filteredPanels) || len(filteredPanels) == 0 {
					return m, nil
				}
				var sessionSectionCount, recentCount int
				for _, p := range filteredPanels {
					if !p.IsRecent && (p.HasSession || p.IsOrphan) {
						sessionSectionCount++
					} else if p.IsRecent {
						recentCount++
					}
				}
				currentPanel := filteredPanels[m.grid.Index]
				if !currentPanel.IsRecent {
					localIdx := m.grid.Index
					col := localIdx % m.grid.Cols
					if localIdx+m.grid.Cols < sessionSectionCount {
						m.grid.Index += m.grid.Cols
					} else if recentCount > 0 {
						targetIdx := sessionSectionCount + col
						if targetIdx >= sessionSectionCount+recentCount {
							targetIdx = sessionSectionCount + recentCount - 1
						}
						m.grid.Index = targetIdx
					} else if len(m.grid.FilteredAvailable()) > 0 {
						m.grid.InAvailable = true
						m.grid.AvailIdx = col
						if m.grid.AvailIdx >= len(m.grid.FilteredAvailable()) {
							m.grid.AvailIdx = len(m.grid.FilteredAvailable()) - 1
						}
					}
				} else {
					localIdx := m.grid.Index - sessionSectionCount
					col := localIdx % m.grid.Cols
					if localIdx+m.grid.Cols < recentCount {
						m.grid.Index += m.grid.Cols
					} else if len(m.grid.FilteredAvailable()) > 0 {
						m.grid.InAvailable = true
						m.grid.AvailIdx = col
						if m.grid.AvailIdx >= len(m.grid.FilteredAvailable()) {
							m.grid.AvailIdx = len(m.grid.FilteredAvailable()) - 1
						}
					}
				}
				m.grid.UpdateScroll(m.width, m.height)
				return m, nil
			}
		case "pgdown", "ctrl+f":
			if m.nav.State == stateGridView {
				for i := 0; i < 3; i++ {
					if m.grid.InAvailable {
						if m.grid.AvailIdx+m.grid.Cols < len(m.grid.FilteredAvailable()) {
							m.grid.AvailIdx += m.grid.Cols
						}
					} else if m.grid.Index >= 0 {
						filteredLen := len(m.grid.FilteredPanels())
						if m.grid.Index+m.grid.Cols < filteredLen {
							m.grid.Index += m.grid.Cols
						} else if len(m.grid.FilteredAvailable()) > 0 {
							m.grid.InAvailable = true
							m.grid.AvailIdx = 0
						}
					}
				}
				m.grid.UpdateScroll(m.width, m.height)
				return m, nil
			}
		case "pgup", "ctrl+b":
			if m.nav.State == stateGridView {
				for i := 0; i < 3; i++ {
					if m.grid.InAvailable {
						if m.grid.AvailIdx >= m.grid.Cols {
							m.grid.AvailIdx -= m.grid.Cols
						} else {
							filteredLen := len(m.grid.FilteredPanels())
							if filteredLen > 0 {
								m.grid.InAvailable = false
								m.grid.Index = filteredLen - 1
							}
						}
					} else if m.grid.Index >= m.grid.Cols {
						m.grid.Index -= m.grid.Cols
					}
				}
				m.grid.UpdateScroll(m.width, m.height)
				return m, nil
			}
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			if m.nav.State == stateGridView {
				if m.grid.Filtering {
					m.grid.Filter += msg.String()
					m.grid.Index = 0
					return m, nil
				}
				filteredPanels := m.grid.FilteredPanels()
				idx := int(msg.String()[0] - '1')
				if idx >= 0 && idx < len(filteredPanels) {
					panel := filteredPanels[idx]
					if panel.IsOrphan {
						m.pending.Name = panel.SessionName
						m.nav.PrevState = stateGridView
						if m.nav.GlobalMode {
							m.menu.SetItems(globalOrphanMenuItems())
						} else {
							m.menu.SetItems(orphanMenuItems())
						}
						m.menu.Select(0)
						m.nav.State = stateOrphanMenu
						return m, nil
					}
					if panel.HasSession {
						m.jumpTarget = &JumpTarget{SessionName: panel.SessionName, Path: panel.Path}
						return m, tea.Quit
					}
				}
				return m, nil
			}
		case "backspace":
			if m.nav.State == stateGridView && m.grid.Filtering {
				if len(m.grid.Filter) > 0 {
					m.grid.Filter = m.grid.Filter[:len(m.grid.Filter)-1]
					m.grid.Index = 0
				}
				return m, nil
			}
		case "r":
			if m.nav.State == stateGridView && m.grid.Filtering {
				m.grid.Filter += "r"
				m.grid.Index = 0
				return m, nil
			}
			m.toast = &toast{message: "Refreshing...", kind: toastInfo, expiresAt: time.Now().Add(toastDuration)}
			m.refreshInFlight++
			if m.nav.GlobalMode {
				return m, tea.Batch(loadGlobalDataCmd(m.deps.Cfg, m.deps.Tmux), toastExpireCmd())
			}
			if m.deps.Svc == nil {
				m.refreshInFlight--
				return m, toastExpireCmd()
			}
			return m, tea.Batch(loadDataCmd(m.deps.Svc), toastExpireCmd())
		default:
			if m.nav.State == stateGridView && m.grid.Filtering {
				key := msg.String()
				if len(key) == 1 {
					ch := key[0]
					if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
						(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' {
						m.grid.Filter += strings.ToLower(key)
						m.grid.Index = 0
						m.grid.InAvailable = false
						m.grid.AvailIdx = 0
						return m, nil
					}
				}
			}
		}
	case tea.MouseMsg:
		// not used
	}

	if m.nav.State == stateGridView || m.nav.State == stateGridDetail {
		m.grid.UpdateScroll(m.width, m.height)
	}
	
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

func (m *model) buildGridPanels() {
	m.grid.Panels = []views.GridPanel{}
	m.grid.Available = []views.GridPanel{}
	m.grid.InvalidateFilterCache()
	
	if m.nav.GlobalMode {
		for _, wt := range m.data.GlobalWorktrees {
			sessionName := wt.Worktree.Name
			if m.deps.Tmux.HasSession(sessionName) {
				panel := views.GridPanel{
					Name:        wt.RepoName + "/" + wt.Worktree.Name,
					SessionName: sessionName,
					Path:        wt.Worktree.Path,
					Branch:      wt.Worktree.Branch,
					HasSession:  true,
				}
				if info, err := m.deps.Tmux.SessionInfo(sessionName); err == nil && info != nil {
					panel.Windows = info.Windows
					panel.Panes = info.Panes
				}
				m.grid.Panels = append(m.grid.Panels, panel)
			} else {
				m.grid.Available = append(m.grid.Available, views.GridPanel{
					Name:       wt.RepoName + "/" + wt.Worktree.Name,
					Path:       wt.Worktree.Path,
					Branch:     wt.Worktree.Branch,
					HasSession: false,
				})
			}
		}
	} else {
		for _, st := range m.data.States {
			if st.HasSession {
				panel := views.GridPanel{
					Name:        st.Worktree.Name,
					SessionName: st.SessionName,
					Path:        st.Worktree.Path,
					Branch:      st.Worktree.Branch,
					HasSession:  true,
				}
				if st.Status != nil {
					panel.Modified = st.Status.Modified
					panel.Staged = st.Status.Staged
				}
				if st.SessionInfo != nil {
					panel.Windows = st.SessionInfo.Windows
					panel.Panes = st.SessionInfo.Panes
				}
				panel.Processes = st.Processes
				m.grid.Panels = append(m.grid.Panels, panel)
			} else {
				panel := views.GridPanel{
					Name:       st.Worktree.Name,
					Path:       st.Worktree.Path,
					Branch:     st.Worktree.Branch,
					HasSession: false,
				}
				if st.Status != nil {
					panel.Modified = st.Status.Modified
					panel.Staged = st.Status.Staged
				}
				m.grid.Available = append(m.grid.Available, panel)
			}
		}
	}

	sortedOrphans := make([]string, len(m.data.Orphans))
	copy(sortedOrphans, m.data.Orphans)
	sort.Strings(sortedOrphans)
	for _, o := range sortedOrphans {
		panel := views.GridPanel{
			Name:        o,
			SessionName: o,
			HasSession:  true,
			IsOrphan:    true,
		}
		if info, err := m.deps.Tmux.SessionInfo(o); err == nil && info != nil {
			panel.Windows = info.Windows
			panel.Panes = info.Panes
		}
		m.grid.Panels = append(m.grid.Panels, panel)
	}

	if !m.nav.GlobalMode {
		for _, r := range m.data.RecentEntries {
			sessionName := r.SessionName
			if sessionName == "" {
				sessionName = r.Worktree
			}
			panel := views.GridPanel{
				Name:        r.RepoName + "/" + r.Worktree,
				SessionName: sessionName,
				Path:        r.Path,
				HasSession:  m.deps.Tmux.HasSession(sessionName),
				IsRecent:    true,
			}
			if panel.HasSession {
				if info, err := m.deps.Tmux.SessionInfo(sessionName); err == nil && info != nil {
					panel.Windows = info.Windows
					panel.Panes = info.Panes
				}
			}
			m.grid.Panels = append(m.grid.Panels, panel)
		}
	}
	
	gridWidth := m.width - 4
	if gridWidth < 32 {
		gridWidth = 32
	}
	minPanelWidth := 32
	m.grid.Cols = gridWidth / minPanelWidth
	if m.grid.Cols < 1 {
		m.grid.Cols = 1
	}
	if m.grid.Cols > 4 {
		m.grid.Cols = 4
	}
}

func (m *model) loadGridContentCmd() tea.Cmd {
	panels := m.grid.Panels
	tmux := m.deps.Tmux
	return func() tea.Msg {
		contents := make(map[string]string)
		for _, p := range panels {
			if p.HasSession {
				content, err := tmux.CapturePane(p.SessionName, 8)
				if err == nil {
					contents[p.SessionName] = content
				}
			}
		}
		return views.GridContentMsg{Contents: contents}
	}
}

func (m *model) openCommandPalette() tea.Cmd {
	items := m.commandPaletteItems()
	listItems := make([]list.Item, len(items))
	for i, item := range items {
		listItems[i] = item
	}
	m.commandPalette.SetItems(listItems)
	m.commandPalette.ResetFilter()
	m.commandPalette.SetFilterState(list.Filtering)
	m.nav.State = stateCommandPalette
	return nil
}

func (m *model) commandPaletteItems() []CommandItem {
	items := []CommandItem{
		{label: "Create worktree", desc: "Create new worktree from branch", run: func(m *model) tea.Cmd {
			if m.nav.GlobalMode {
				repos := views.ExtractUniqueRepos(m.data.GlobalWorktrees)
				if len(repos) == 0 {
					m.toast = &toast{message: "No repositories found", kind: toastError, expiresAt: time.Now().Add(toastDuration)}
					return toastExpireCmd()
				}
				m.data.AvailableRepos = repos
				items := make([]list.Item, len(repos))
				for i, r := range repos {
					items[i] = listItem{title: r.Name, desc: r.Root, kind: kindHeader}
				}
				m.menu.SetItems(items)
				m.menu.Select(0)
				m.nav.State = stateSelectRepo
				return nil
			}
			m.nav.State = stateCreateName
			m.input.SetValue("")
			return m.input.Focus()
		}},
		{label: "Toggle global mode", desc: "Switch between repo and global view", run: func(m *model) tea.Cmd {
			m.nav.GlobalMode = !m.nav.GlobalMode
			m.nav.Loading = true
			if m.nav.GlobalMode {
				return tea.Batch(m.spinner.Tick, loadGlobalDataCmd(m.deps.Cfg, m.deps.Tmux))
			}
			if m.nav.InGitRepo {
				return tea.Batch(m.spinner.Tick, loadDataCmd(m.deps.Svc))
			}
			m.nav.GlobalMode = true
			return tea.Batch(m.spinner.Tick, loadGlobalDataCmd(m.deps.Cfg, m.deps.Tmux))
		}},
		{label: "Refresh", desc: "Reload worktree and session data", run: func(m *model) tea.Cmd {
			m.toast = &toast{message: "Refreshing...", kind: toastInfo, expiresAt: time.Now().Add(toastDuration)}
			m.refreshInFlight++
			if m.nav.GlobalMode {
				return tea.Batch(loadGlobalDataCmd(m.deps.Cfg, m.deps.Tmux), toastExpireCmd())
			}
			if m.deps.Svc == nil {
				m.refreshInFlight--
				return toastExpireCmd()
			}
			return tea.Batch(loadDataCmd(m.deps.Svc), toastExpireCmd())
		}},
		{label: "Show help", desc: "Display keybindings and commands", run: func(m *model) tea.Cmd {
			m.nav.State = stateHelp
			return nil
		}},
		{label: "Quit", desc: "Exit treemux", run: func(m *model) tea.Cmd {
			return tea.Quit
		}},
	}

	if sel, ok := m.list.SelectedItem().(listItem); ok {
		switch sel.kind {
		case kindWorktree:
			if m.deps.Svc == nil {
				break
			}
			wt := sel.data.(workspace.WorktreeState)
			items = append(items,
				CommandItem{label: "Jump to worktree", desc: "Switch to selected worktree session", run: func(m *model) tea.Cmd {
					if m.deps.Svc == nil {
						return nil
					}
					sessionName := m.deps.Svc.SessionName(wt.Worktree.Path)
					if !m.deps.Svc.Tmux.HasSession(sessionName) {
						_ = m.deps.Svc.Tmux.NewSession(sessionName, wt.Worktree.Path)
					}
					if m.deps.RecentStore != nil && m.deps.Svc.Git != nil {
						m.deps.RecentStore.Add(m.deps.Svc.Git.RepoRoot, wt.Worktree.Name, sessionName, wt.Worktree.Path)
						_ = m.deps.RecentStore.Save()
					}
					m.jumpTarget = &JumpTarget{SessionName: sessionName, Path: wt.Worktree.Path}
					return tea.Quit
				}},
				CommandItem{label: "Delete worktree", desc: "Remove worktree and kill session", run: func(m *model) tea.Cmd {
					if m.deps.Svc == nil || m.deps.Svc.Git == nil {
						return nil
					}
					if wt.Worktree.Path == m.deps.Svc.Git.RepoRoot {
						m.toast = &toast{message: "Cannot delete current worktree", kind: toastError, expiresAt: time.Now().Add(toastDuration)}
						return toastExpireCmd()
					}
					return deleteWorktreeCmd(m.deps.Svc, wt.Worktree.Path)
				}},
			)
			if wt.HasSession {
				items = append(items, CommandItem{label: "Kill session", desc: "Kill tmux session only", run: func(m *model) tea.Cmd {
					if m.deps.Svc == nil {
						return nil
					}
					return killSessionCmd(m.deps.Svc, wt.SessionName)
				}})
			}
		case kindGlobal:
			wt := sel.data.(scanner.RepoWorktree)
			sessionName := wt.Worktree.Name
			items = append(items,
				CommandItem{label: "Jump to worktree", desc: "Switch to selected worktree session", run: func(m *model) tea.Cmd {
					if !m.deps.Tmux.HasSession(sessionName) {
						_ = m.deps.Tmux.NewSession(sessionName, wt.Worktree.Path)
					}
					m.jumpTarget = &JumpTarget{SessionName: sessionName, Path: wt.Worktree.Path}
					return tea.Quit
				}},
			)
		case kindOrphan:
			sessionName := sel.title
			items = append(items, CommandItem{label: "Kill orphan session", desc: "Kill this orphaned session", run: func(m *model) tea.Cmd {
				if m.nav.GlobalMode {
					return killSessionDirectCmd(m.deps.Tmux, sessionName)
				}
				if m.deps.Svc == nil {
					return nil
				}
				return killSessionCmd(m.deps.Svc, sessionName)
			}})
		}
	}

	return items
}

// View

func (m model) View() string {
	if m.nav.Loading {
		loadingBox := lipgloss.NewStyle().
			Padding(2, 4).
			Render(m.spinner.View() + " Loading worktrees...")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, loadingBox)
	}

	if m.nav.State == stateHelp {
		return renderHelp()
	}

	switch m.nav.State {
	case stateSelectRepo:
		return views.RenderRepoSelector(&m.menu)
	case stateCreateName:
		return views.RenderNameInput(m.input.View())
	case stateCreateBranch:
		return views.RenderBranchSelector(&m.menu)
	case stateOrphanBranch:
		return views.RenderMenu("Adopt: base branch", &m.menu)
	case stateActionMenu:
		return views.RenderMenu("Actions", &m.menu)
	case stateOrphanMenu:
		return views.RenderMenu("Orphaned session", &m.menu)
	case stateCommandPalette:
		return renderCommandPalette(&m.commandPalette, m.width, m.height)
	case stateGridView:
		return m.grid.RenderView(m.width, m.height)
	case stateGridDetail:
		return m.grid.RenderDetail(m.width, m.height)
	}

	left := theme.ListFrameStyle.Render(m.list.View())

	previewContent := m.getPreviewContent()
	right := theme.PreviewFrameStyle.Render(previewContent)

	if m.toast != nil && !m.toast.expired() {
		styles := toastStyles{
			success: theme.SuccessStyle.Bold(true),
			error:   theme.ErrorStyle.Bold(true),
			warning: theme.WarnStyle.Bold(true),
			info:    theme.SectionStyle.Bold(true),
		}
		right = m.toast.render(styles) + "\n\n" + right
	}

	logoStyle := lipgloss.NewStyle().Foreground(theme.SuccessColor).Bold(true)
	t1 := lipgloss.NewStyle().Foreground(theme.Flamingo).Bold(true)
	t2 := lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)
	t3 := lipgloss.NewStyle().Foreground(theme.Lavender).Bold(true)
	t4 := lipgloss.NewStyle().Foreground(theme.Accent2).Bold(true)

	gradientTitle := logoStyle.Render("▲ ") +
		t1.Render("tre") +
		t2.Render("em") +
		t3.Render("u") +
		t4.Render("x")

	repoIndicator := ""
	if m.nav.GlobalMode {
		repoIndicator = theme.WarnStyle.Render("GLOBAL")
	} else if m.deps.Svc != nil && m.deps.Svc.Git != nil {
		repoName := filepath.Base(m.deps.Svc.Git.RepoRoot)
		repoIndicator = theme.SectionStyle.Render(repoName)
	}

	headerWidth := m.width - 6
	if headerWidth < 20 {
		headerWidth = 20
	}
	titleLen := 12
	repoLen := len(repoIndicator)
	padding := headerWidth - titleLen - repoLen
	if padding < 1 {
		padding = 1
	}

	headerLine := gradientTitle + strings.Repeat(" ", padding) + repoIndicator
	
	gradientChars := "━"
	dividerParts := []string{}
	colors := []lipgloss.Color{theme.Flamingo, theme.Accent, theme.Lavender, theme.Accent2, theme.Teal}
	segmentLen := headerWidth / len(colors)
	for i, c := range colors {
		length := segmentLen
		if i == len(colors)-1 {
			length = headerWidth - (segmentLen * (len(colors) - 1))
		}
		dividerParts = append(dividerParts, lipgloss.NewStyle().Foreground(c).Render(strings.Repeat(gradientChars, length)))
	}
	dividerLine := strings.Join(dividerParts, "")

	headerBox := lipgloss.NewStyle().
		Padding(0, 2).
		MarginTop(1).
		Render(headerLine + "\n" + dividerLine)

	toggleHint := theme.KeyStyle.Render("g") + theme.DimStyle.Render(" global  ")
	if m.nav.GlobalMode {
		toggleHint = theme.KeyStyle.Render("g") + theme.DimStyle.Render(" repo  ")
	}

	sep := lipgloss.NewStyle().Foreground(theme.OverlayColor).Render(" ┃ ")
	footerContent := theme.KeyStyle.Render("enter") + theme.DimStyle.Render(" select  ") +
		theme.KeyStyle.Render("/") + theme.DimStyle.Render(" filter") +
		sep +
		theme.KeyStyle.Render("ctrl+g") + theme.DimStyle.Render(" grid view  ") +
		theme.KeyStyle.Render("ctrl+p") + theme.DimStyle.Render(" cmd  ") +
		toggleHint +
		sep +
		theme.KeyStyle.Render("?") + theme.DimStyle.Render(" help  ") +
		theme.KeyStyle.Render("q") + theme.DimStyle.Render(" quit")
	
	footer := lipgloss.NewStyle().
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.OverlayColor).
		Padding(0, 2).
		Foreground(theme.SubTextColor).
		Render(footerContent)

	sepHeight := m.height - 7
	if sepHeight < 1 {
		sepHeight = 1
	}
	sepColors := []lipgloss.Color{theme.Accent, theme.Accent2, theme.Teal, theme.Accent2, theme.Accent}
	sepLines := []string{}
	for i := 0; i < sepHeight; i++ {
		colorIdx := i % len(sepColors)
		sepLines = append(sepLines, lipgloss.NewStyle().Foreground(sepColors[colorIdx]).Render("│"))
	}
	separator := strings.Join(sepLines, "\n")
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
	hasActiveSessions := len(orphans) > 0
	if !hasActiveSessions {
		for _, st := range states {
			if st.HasSession {
				hasActiveSessions = true
				break
			}
		}
	}
	if hasActiveSessions {
		items = append(items, listItem{
			title: "Grid View",
			kind:  kindGridView,
		})
	}
	if len(states) > 0 {
		items = append(items, listItem{title: "WORKTREES", kind: kindHeader})
		for _, st := range states {
			items = append(items, listItem{
				title:     st.Worktree.Name,
				kind:      kindWorktree,
				data:      st,
				isCurrent: st.Worktree.Path == currentPath,
			})
		}
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

func buildGlobalItems(worktrees []scanner.RepoWorktree, orphans []string, tmux *tmux.Tmux) []list.Item {
	items := []list.Item{}
	items = append(items, listItem{
		title: "+ New Worktree",
		desc:  "Create worktree and session",
		kind:  kindCreate,
	})
	if len(orphans) > 0 || len(worktrees) > 0 {
		items = append(items, listItem{
			title: "Grid View",
			desc:  "View all sessions",
			kind:  kindGridView,
		})
	}

	var withSession, withoutSession []scanner.RepoWorktree
	for _, wt := range worktrees {
		if tmux.HasSession(wt.Worktree.Name) {
			withSession = append(withSession, wt)
		} else {
			withoutSession = append(withoutSession, wt)
		}
	}

	if len(withSession) > 0 {
		items = append(items, listItem{title: "SESSIONS", kind: kindHeader})
		for _, wt := range withSession {
			items = append(items, listItem{
				title: wt.RepoName + "/" + wt.Worktree.Name,
				desc:  wt.Worktree.Branch,
				kind:  kindGlobal,
				data:  wt,
			})
		}
	}

	if len(withoutSession) > 0 {
		if len(withSession) > 0 {
			items = append(items, listItem{kind: kindSeparator})
		}
		items = append(items, listItem{title: "AVAILABLE WORKTREES", kind: kindHeader})
		for _, wt := range withoutSession {
			items = append(items, listItem{
				title: wt.RepoName + "/" + wt.Worktree.Name,
				desc:  wt.Worktree.Branch,
				kind:  kindGlobal,
				data:  wt,
			})
		}
	}

	if len(orphans) > 0 {
		items = append(items, listItem{kind: kindSeparator})
		items = append(items, listItem{title: "ORPHANED SESSIONS (no worktree)", kind: kindHeader})
		for _, o := range orphans {
			items = append(items, listItem{
				title: o,
				desc:  "orphaned session",
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

func (m *model) getPreviewContent() string {
	sel, ok := m.list.SelectedItem().(listItem)
	if !ok {
		return theme.DimStyle.Render("Select a worktree")
	}

	ctx := components.PreviewContext{
		Width:           m.preview.Width,
		PaneContent:     m.paneContent,
		PaneSession:     m.paneSession,
		GlobalMode:      m.nav.GlobalMode,
		Tmux:            m.deps.Tmux,
		States:          m.data.States,
		Orphans:         m.data.Orphans,
		GlobalWorktrees: m.data.GlobalWorktrees,
	}

	item := components.PreviewItem{
		Kind:  components.ItemKind(sel.kind),
		Title: sel.title,
		Data:  sel.data,
	}

	return components.RenderPreview(ctx, item)
}

func renderCommandPalette(m *list.Model, width, height int) string {
	header := theme.TitleStyle.Render("  Command Palette")
	divider := theme.SeparatorStyle.Render("────────────────────────────────")

	paletteWidth := 60
	if width > 0 && width < paletteWidth+10 {
		paletteWidth = width - 10
	}
	paletteHeight := 20
	if height > 0 && height < paletteHeight+6 {
		paletteHeight = height - 6
	}
	m.SetSize(paletteWidth-4, paletteHeight-4)

	content := header + "\n" + divider + "\n\n" + m.View()
	return theme.ModalStyle.Width(paletteWidth).Render(content)
}

func renderHelp() string {
	helpLine := func(key, desc string) string {
		k := lipgloss.NewStyle().
			Foreground(theme.BaseBg).
			Background(theme.Teal).
			Bold(true).
			Padding(0, 1).
			Width(10).
			Render(key)
		d := lipgloss.NewStyle().Foreground(theme.TextColor).Render("  " + desc)
		return k + d
	}
	
	sectionHeader := func(title string) string {
		return lipgloss.NewStyle().
			Foreground(theme.Accent).
			Bold(true).
			MarginTop(1).
			Render("━━ " + title + " ━━")
	}

	t1 := lipgloss.NewStyle().Foreground(theme.Flamingo).Bold(true)
	t2 := lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)
	t3 := lipgloss.NewStyle().Foreground(theme.Accent2).Bold(true)
	title := lipgloss.NewStyle().Foreground(theme.SuccessColor).Bold(true).Render("▲ ") +
		t1.Render("tree") + t2.Render("mu") + t3.Render("x") +
		theme.DimStyle.Render(" help")

	content := strings.Join([]string{
		title,
		sectionHeader("Navigation"),
		helpLine("j / ↓", "move down"),
		helpLine("k / ↑", "move up"),
		helpLine("enter", "jump to worktree / create"),
		helpLine("/", "filter list"),
		sectionHeader("Actions"),
		helpLine("tab", "open actions menu"),
		helpLine("ctrl+p", "command palette"),
		helpLine("ctrl+g", "grid view (sessions)"),
		helpLine("ctrl+d", "delete worktree + session"),
		helpLine("r", "refresh"),
		sectionHeader("Modes"),
		helpLine("g", "toggle global mode"),
		sectionHeader("Other"),
		helpLine("?", "toggle this help"),
		helpLine("esc / q", "quit (back in dialogs)"),
	}, "\n")
	return theme.ModalStyle.Render(content)
}

type key struct {
	k string
	d string
}

func actionMenuItems(hasSession bool) []list.Item {
	items := []list.Item{
		listItem{title: theme.IconJump + "  Jump", desc: "Switch to session", kind: kindHeader},
		listItem{title: theme.IconDelete + "  Delete worktree", desc: "Delete worktree + session", kind: kindHeader},
	}
	if hasSession {
		items = append(items, listItem{title: theme.IconKill + "  Kill session", desc: "Kill tmux session only", kind: kindHeader})
	}
	return items
}

func orphanMenuItems() []list.Item {
	return []list.Item{
		listItem{title: theme.IconJump + "  Jump", desc: "Switch to session", kind: kindHeader},
		listItem{title: theme.IconAdopt + "  Adopt", desc: "Create worktree for this session", kind: kindHeader},
		listItem{title: theme.IconKill + "  Kill session", desc: "Kill orphaned session", kind: kindHeader},
	}
}

func globalActionMenuItems() []list.Item {
	return []list.Item{
		listItem{title: theme.IconJump + "  Jump", desc: "Switch to session", kind: kindHeader},
	}
}

func globalOrphanMenuItems() []list.Item {
	return []list.Item{
		listItem{title: theme.IconJump + "  Jump", desc: "Switch to session", kind: kindHeader},
		listItem{title: theme.IconKill + "  Kill session", desc: "Kill orphaned session", kind: kindHeader},
	}
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

type itemDelegate struct {
	listWidth int
}

func (d itemDelegate) Height() int                             { return 2 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	i, ok := item.(listItem)
	if !ok {
		return
	}

	selected := index == m.Index()
	width := d.listWidth - 4
	if width < 20 {
		width = 20
	}

	var accentColor lipgloss.Color
	switch i.kind {
	case kindCreate:
		accentColor = theme.Teal
	case kindGridView:
		accentColor = theme.Accent2
	case kindWorktree:
		accentColor = theme.Accent
	case kindOrphan:
		accentColor = theme.Peach
	case kindRecent:
		accentColor = theme.Accent2
	case kindGlobal:
		accentColor = theme.Teal
	default:
		accentColor = theme.DimColor
	}

	accentBar := theme.DimStyle.Render("  ")
	if selected {
		accentBar = lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("▌ ")
	}

	var line1, line2 string

	switch i.kind {
	case kindCreate:
		if selected {
			line1 = accentBar + lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("+ New Worktree")
			line2 = accentBar + theme.SelectedBranchStyle.Render("  Create worktree and session")
		} else {
			line1 = accentBar + theme.TextStyle.Render("+ New Worktree")
			line2 = accentBar + theme.DimStyle.Render("  Create worktree and session")
		}

	case kindGridView:
		icon := "◫"
		if selected {
			line1 = accentBar + lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render(icon + " Grid View")
			line2 = accentBar + lipgloss.NewStyle().Foreground(accentColor).Render("  View all sessions")
		} else {
			line1 = accentBar + theme.TextStyle.Render(icon + " Grid View")
			line2 = accentBar + theme.DimStyle.Render("  View all sessions")
		}

	case kindWorktree:
		wt := i.data.(workspace.WorktreeState)
		name := wt.Worktree.Name
		branch := wt.Worktree.Branch

		statusBadge := theme.SuccessStyle.Render(theme.IconClean)
		if wt.Status != nil && !wt.Status.Clean {
			badgeParts := []string{}
			if wt.Status.Modified > 0 {
				badgeParts = append(badgeParts, theme.WarnStyle.Render(fmt.Sprintf("%dM", wt.Status.Modified)))
			}
			if wt.Status.Staged > 0 {
				badgeParts = append(badgeParts, theme.SectionStyle.Render(fmt.Sprintf("%dS", wt.Status.Staged)))
			}
			if len(badgeParts) > 0 {
				statusBadge = strings.Join(badgeParts, " ")
			} else if wt.Status.Untracked > 0 {
				statusBadge = theme.DimStyle.Render(fmt.Sprintf("%d?", wt.Status.Untracked))
			}
		}

		indicator := " "
		if i.isCurrent {
			indicator = theme.IconCurrent
		}

		sessionInfo := ""
		if wt.SessionInfo != nil {
			if wt.SessionInfo.IsActive {
				sessionInfo = " " + theme.LiveBadgeStyle.Render("LIVE")
			} else {
				sessionInfo = theme.DimStyle.Render(fmt.Sprintf(" %dw %dp", wt.SessionInfo.Windows, wt.SessionInfo.Panes))
			}
		}

		nameDisplay := name
		maxNameWidth := width - 14
		if maxNameWidth < 10 {
			maxNameWidth = 10
		}
		if len(nameDisplay) > maxNameWidth {
			nameDisplay = nameDisplay[:maxNameWidth-1] + "…"
		}

		branchDisplay := branch
		maxBranchWidth := width - 16
		if maxBranchWidth < 10 {
			maxBranchWidth = 10
		}
		if len(branchDisplay) > maxBranchWidth {
			branchDisplay = branchDisplay[:maxBranchWidth-1] + "…"
		}

		selStyle := lipgloss.NewStyle().Foreground(accentColor).Bold(true)
		if selected {
			line1 = accentBar + selStyle.Render(indicator+" "+nameDisplay) + "  " + statusBadge
			line2 = accentBar + "    " + theme.SelectedBranchStyle.Render(theme.IconBranch+" "+branchDisplay) + sessionInfo
		} else {
			line1 = accentBar + theme.TextStyle.Render(indicator+" "+nameDisplay) + "  " + statusBadge
			line2 = accentBar + "    " + theme.BranchStyle.Render(theme.IconBranch+" "+branchDisplay) + sessionInfo
		}

	case kindSeparator:
		line1 = ""
		line2 = ""

	case kindHeader:
		label := i.title
		suffix := ""
		labelStyle := theme.SectionStyle
		if i.title == "ORPHANED SESSIONS" {
			suffix = " (no worktree)"
			labelStyle = theme.WarnStyle
		} else if i.title == "RECENT" {
			suffix = " (other projects)"
		}
		fullLabel := " " + label + suffix + " "
		labelLen := len(label) + len(suffix) + 2
		sideLen := (width - labelLen) / 2
		if sideLen < 2 {
			sideLen = 2
		}
		leftBar := strings.Repeat("─", sideLen)
		rightBar := strings.Repeat("─", sideLen)
		line1 = theme.DimStyle.Render(leftBar) + labelStyle.Render(fullLabel) + theme.DimStyle.Render(rightBar)
		line2 = ""

	case kindOrphan:
		name := i.title
		orphanStyle := lipgloss.NewStyle().Foreground(theme.Peach).Bold(true)
		if selected {
			line1 = accentBar + orphanStyle.Render(theme.IconSession+" "+name)
			line2 = accentBar + "    " + lipgloss.NewStyle().Foreground(theme.Peach).Render("orphaned session")
		} else {
			line1 = accentBar + lipgloss.NewStyle().Foreground(theme.Peach).Render(theme.IconSession+" "+name)
			line2 = accentBar + "    " + theme.DimStyle.Render("orphaned session")
		}

	case kindRecent:
		name := i.title
		recentStyle := lipgloss.NewStyle().Foreground(theme.Accent2).Bold(true)
		if selected {
			line1 = accentBar + recentStyle.Render(theme.IconJump+" "+name)
			line2 = accentBar + "    " + lipgloss.NewStyle().Foreground(theme.Accent2).Render("recent project")
		} else {
			line1 = accentBar + lipgloss.NewStyle().Foreground(theme.Accent2).Render(theme.IconJump+" "+name)
			line2 = accentBar + "    " + theme.DimStyle.Render("recent project")
		}

	case kindRepoHeader:
		label := i.title
		fullLabel := " " + theme.IconPath + " " + label + " "
		labelLen := len(label) + 5
		sideLen := (width - labelLen) / 2
		if sideLen < 2 {
			sideLen = 2
		}
		leftBar := strings.Repeat("─", sideLen)
		rightBar := strings.Repeat("─", sideLen)
		line1 = theme.DimStyle.Render(leftBar) + theme.SectionStyle.Render(fullLabel) + theme.DimStyle.Render(rightBar)
		line2 = ""

	case kindGlobal:
		wt := i.data.(scanner.RepoWorktree)
		name := wt.Worktree.Name
		branch := wt.Worktree.Branch

		nameDisplay := name
		maxNameWidth := width - 8
		if maxNameWidth < 10 {
			maxNameWidth = 10
		}
		if len(nameDisplay) > maxNameWidth {
			nameDisplay = nameDisplay[:maxNameWidth-1] + "…"
		}

		branchDisplay := branch
		maxBranchWidth := width - 12
		if maxBranchWidth < 10 {
			maxBranchWidth = 10
		}
		if len(branchDisplay) > maxBranchWidth {
			branchDisplay = branchDisplay[:maxBranchWidth-1] + "…"
		}

		globalStyle := lipgloss.NewStyle().Foreground(theme.Teal).Bold(true)
		if selected {
			line1 = accentBar + globalStyle.Render(theme.IconWorktree+" "+nameDisplay)
			line2 = accentBar + "    " + lipgloss.NewStyle().Foreground(theme.Teal).Render(theme.IconBranch+" "+branchDisplay)
		} else {
			line1 = accentBar + theme.TextStyle.Render(theme.IconWorktree+" "+nameDisplay)
			line2 = accentBar + "    " + theme.BranchStyle.Render(theme.IconBranch+" "+branchDisplay)
		}
	}

	if selected && i.kind != kindSeparator && i.kind != kindHeader && i.kind != kindRepoHeader {
		rowStyle := lipgloss.NewStyle().Background(theme.SurfaceBg).Width(width)
		if line2 != "" {
			fmt.Fprint(w, rowStyle.Render(line1)+"\n"+rowStyle.Render(line2))
		} else {
			fmt.Fprint(w, rowStyle.Render(line1)+"\n")
		}
	} else {
		if line2 != "" {
			fmt.Fprint(w, line1+"\n"+line2)
		} else {
			fmt.Fprint(w, line1+"\n")
		}
	}
}

func newItemDelegate(width int) itemDelegate {
	return itemDelegate{listWidth: width}
}
