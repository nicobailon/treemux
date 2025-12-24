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
	commandPalette  list.Model
	states          []workspace.WorktreeState
	orphans         []string
	recentEntries   []recent.Entry
	globalWorktrees []scanner.RepoWorktree
	width           int
	height          int
	toast           *toast
	pending         string
	pendingWT       *workspace.WorktreeState
	pendingGlobal   *scanner.RepoWorktree
	prevState       viewState
	loading         bool
	filtering       bool
	jumpTarget       *JumpTarget
	globalMode       bool
	inGitRepo        bool
	selectAfterLoad  string
	pendingCreateSvc *workspace.Service
	availableRepos   []repoInfo
	refreshInterval  time.Duration
	refreshInFlight  int
	paneContent      string
	paneSession      string
	gridIndex        int
	gridCols         int
	gridPanels       []gridPanel
	gridAvailable    []gridPanel
	gridFilter       string
	gridFiltering    bool
	gridInAvailable  bool
	gridAvailIdx     int
	gridViewport     viewport.Model
	gridScrollOffset int
	gridDetailPanel  *gridPanel
	gridDetailIdx    int
}

type gridPanel struct {
	name        string
	sessionName string
	path        string
	branch      string
	content     string
	hasSession  bool
	isOrphan    bool
	isRecent    bool
	modified    int
	staged      int
	windows     int
	panes       int
	processes   []string
}

type JumpTarget struct {
	SessionName string
	Path        string
	Create      bool
}

type repoInfo struct {
	name string
	root string
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

	cmdPaletteDel := list.NewDefaultDelegate()
	cmdPaletteDel.ShowDescription = true
	cmdPaletteDel.Styles.NormalTitle = textStyle
	cmdPaletteDel.Styles.NormalDesc = dimStyle
	cmdPaletteDel.Styles.SelectedTitle = currentStyle
	cmdPaletteDel.Styles.SelectedDesc = sectionStyle
	cmdPalette := list.New([]list.Item{}, cmdPaletteDel, 0, 0)
	cmdPalette.DisableQuitKeybindings()
	cmdPalette.SetShowHelp(false)
	cmdPalette.SetShowStatusBar(false)
	cmdPalette.SetFilteringEnabled(true)
	cmdPalette.SetShowTitle(false)
	cmdPalette.SetShowPagination(false)
	cmdPalette.FilterInput.Prompt = "> "
	cmdPalette.FilterInput.PromptStyle = keyStyle
	cmdPalette.FilterInput.TextStyle = textStyle

	return model{
		svc:             svc,
		cfg:             cfg,
		tmux:            t,
		recentStore:     recentStore,
		state:           stateGridView,
		nextBranchState: stateCreateBranch,
		list:            l,
		preview:         vp,
		input:           ti,
		menu:            menu,
		commandPalette:  cmdPalette,
		spinner:         sp,
		loading:         true,
		globalMode:      !inGitRepo,
		inGitRepo:       inGitRepo,
		refreshInterval: defaultRefreshInterval,
	}
}

// TEA plumbing

func (m model) Init() tea.Cmd {
	if m.globalMode {
		return tea.Batch(m.spinner.Tick, loadGlobalDataCmd(m.cfg, m.tmux), m.tickCmd(), m.previewTickCmd())
	}
	return tea.Batch(m.spinner.Tick, loadDataCmd(m.svc), m.tickCmd(), m.previewTickCmd())
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
		if m.refreshInFlight > 0 {
			m.refreshInFlight--
		}
		repoRoot := ""
		if m.svc != nil && m.svc.Git != nil {
			repoRoot = m.svc.Git.RepoRoot
		}
		m.states = reorderCurrentFirst(msg.states, repoRoot)
		m.orphans = msg.orphans
		if m.recentStore != nil && repoRoot != "" {
			m.recentEntries = m.recentStore.GetOtherProjects(repoRoot, 5)
		}
		items := buildItems(m.states, m.orphans, m.recentEntries, repoRoot)
		m.list.SetItems(items)
		if m.selectAfterLoad != "" {
			for i, item := range items {
				if li, ok := item.(listItem); ok && li.kind == kindWorktree && li.title == m.selectAfterLoad {
					m.list.Select(i)
					break
				}
			}
			m.selectAfterLoad = ""
		}
		
		if m.state == stateGridView {
			wasInAvailable := m.gridInAvailable
			prevAvailIdx := m.gridAvailIdx
			prevGridIdx := m.gridIndex
			m.buildGridPanels()
			if wasInAvailable && len(m.gridAvailable) > 0 {
				m.gridInAvailable = true
				if prevAvailIdx < len(m.gridAvailable) {
					m.gridAvailIdx = prevAvailIdx
				} else {
					m.gridAvailIdx = len(m.gridAvailable) - 1
				}
			} else if !wasInAvailable && prevGridIdx >= 0 {
				filteredPanels := m.getFilteredGridPanels()
				if prevGridIdx < len(filteredPanels) {
					m.gridIndex = prevGridIdx
				} else if len(filteredPanels) > 0 {
					m.gridIndex = len(filteredPanels) - 1
				} else if len(m.gridAvailable) > 0 {
					m.gridInAvailable = true
					m.gridAvailIdx = 0
				}
			}
			return m, m.loadGridContentCmd()
		}
		return m, nil

	case globalDataLoadedMsg:
		m.loading = false
		if m.refreshInFlight > 0 {
			m.refreshInFlight--
		}
		m.globalWorktrees = msg.worktrees
		m.orphans = msg.orphans
		items := buildGlobalItems(m.globalWorktrees, m.orphans)
		m.list.SetItems(items)
		if m.selectAfterLoad != "" {
			for i, item := range items {
				if li, ok := item.(listItem); ok && li.kind == kindGlobal && li.title == m.selectAfterLoad {
					m.list.Select(i)
					break
				}
			}
			m.selectAfterLoad = ""
		}
		
		if m.state == stateGridView {
			wasInAvailable := m.gridInAvailable
			prevAvailIdx := m.gridAvailIdx
			prevGridIdx := m.gridIndex
			m.buildGridPanels()
			if wasInAvailable && len(m.gridAvailable) > 0 {
				m.gridInAvailable = true
				if prevAvailIdx < len(m.gridAvailable) {
					m.gridAvailIdx = prevAvailIdx
				} else {
					m.gridAvailIdx = len(m.gridAvailable) - 1
				}
			} else if !wasInAvailable && prevGridIdx >= 0 {
				filteredPanels := m.getFilteredGridPanels()
				if prevGridIdx < len(filteredPanels) {
					m.gridIndex = prevGridIdx
				} else if len(filteredPanels) > 0 {
					m.gridIndex = len(filteredPanels) - 1
				} else if len(m.gridAvailable) > 0 {
					m.gridInAvailable = true
					m.gridAvailIdx = 0
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
		if m.svc != nil && m.svc.Git != nil {
			var currentBranch string
			for _, st := range m.states {
				if st.Worktree.Path == m.svc.Git.RepoRoot {
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
		m.state = m.nextBranchState
		return m, nil

	case jumpMsg:
		m.jumpTarget = &JumpTarget{SessionName: msg.sessionName, Path: msg.path}
		return m, tea.Quit

	case resultMsg:
		if msg.action == "load" {
			m.loading = false
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
			m.state = stateMain
			m.pendingCreateSvc = nil
			if m.globalMode {
				return m, tea.Batch(loadGlobalDataCmd(m.cfg, m.tmux), toastExpireCmd())
			}
			if m.svc == nil {
				return m, toastExpireCmd()
			}
			return m, tea.Batch(loadDataCmd(m.svc), toastExpireCmd())
		}
		return m, nil

	case refreshTickMsg:
		if m.refreshInFlight > 0 || m.loading {
			return m, m.tickCmd()
		}
		m.refreshInFlight++
		if m.globalMode {
			return m, tea.Batch(loadGlobalDataCmd(m.cfg, m.tmux), m.tickCmd())
		}
		if m.svc == nil {
			m.refreshInFlight--
			return m, m.tickCmd()
		}
		return m, tea.Batch(loadDataCmd(m.svc), m.tickCmd())

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
			if m.tmux.HasSession(wt.Worktree.Name) {
				sessionName = wt.Worktree.Name
			}
		case kindOrphan:
			sessionName = sel.title
		}
		if sessionName != "" {
			return m, tea.Batch(loadPaneContentCmd(m.tmux, sessionName, 50), m.previewTickCmd())
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

	case gridContentMsg:
		for i := range m.gridPanels {
			if content, ok := msg.contents[m.gridPanels[i].sessionName]; ok {
				m.gridPanels[i].content = content
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
	switch m.state {
	case stateSelectRepo:
		var cmd tea.Cmd
		m.menu, cmd = m.menu.Update(msg)
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				idx := m.menu.Index()
				if idx >= 0 && idx < len(m.availableRepos) {
					repo := m.availableRepos[idx]
					g := &git.Git{RepoRoot: repo.root}
					m.pendingCreateSvc = workspace.NewService(g, m.tmux, m.cfg)
					m.state = stateCreateName
					m.input.SetValue("")
					return m, m.input.Focus()
				}
			case "esc":
				m.state = stateMain
				m.pendingCreateSvc = nil
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
				m.pending = name
				m.nextBranchState = stateCreateBranch
				svc := m.svc
				if m.pendingCreateSvc != nil {
					svc = m.pendingCreateSvc
				}
				if svc == nil {
					m.state = stateMain
					return m, nil
				}
				return m, branchesCmd(svc)
			case "esc":
				m.state = stateMain
				m.pendingCreateSvc = nil
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
					svc := m.svc
					if m.pendingCreateSvc != nil {
						svc = m.pendingCreateSvc
					}
					if svc == nil {
						m.state = stateMain
						return m, nil
					}
					m.selectAfterLoad = filepath.Base(svc.WorktreePath(name))
					m.state = stateMain
					return m, createWorktreeCmd(svc, name, branch)
				}
			case "esc":
				m.state = stateMain
				m.pendingCreateSvc = nil
			}
		}
		return m, cmd

	case stateOrphanBranch:
		var cmd tea.Cmd
		m.menu, cmd = m.menu.Update(msg)
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				if m.svc == nil {
					m.state = stateMain
					return m, nil
				}
				if sel, ok := m.menu.SelectedItem().(listItem); ok {
					branch := sel.title
					name := m.pending
					m.selectAfterLoad = filepath.Base(m.svc.WorktreePath(name))
					m.state = stateMain
					return m, adoptCmd(m.svc, name, branch)
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
					if m.pendingWT != nil && m.svc != nil {
						sessionName := m.svc.SessionName(m.pendingWT.Worktree.Path)
						if !m.svc.Tmux.HasSession(sessionName) {
							_ = m.svc.Tmux.NewSession(sessionName, m.pendingWT.Worktree.Path)
						}
						if m.recentStore != nil && m.svc.Git != nil {
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
					if m.pendingWT != nil && m.svc != nil && m.svc.Git != nil {
						if m.pendingWT.Worktree.Path == m.svc.Git.RepoRoot {
							m.toast = &toast{message: "Cannot delete current worktree", kind: toastError, expiresAt: time.Now().Add(toastDuration)}
							m.state = stateMain
							return m, toastExpireCmd()
						}
						return m, deleteWorktreeCmd(m.svc, m.pendingWT.Worktree.Path)
					}
				case strings.Contains(title, "Kill session"):
					if m.pendingWT != nil && m.svc != nil {
						return m, killSessionCmd(m.svc, m.pendingWT.SessionName)
					}
					if m.pending != "" {
						if m.globalMode {
							return m, killSessionDirectCmd(m.tmux, m.pending)
						}
						if m.svc != nil {
							return m, killSessionCmd(m.svc, m.pending)
						}
					}
				case strings.Contains(title, "Adopt"):
					if m.pending != "" && m.svc != nil {
						m.nextBranchState = stateOrphanBranch
						return m, branchesCmd(m.svc)
					}
				}
				if m.prevState != 0 {
					m.state = m.prevState
				} else {
					m.state = stateMain
				}
				return m, nil
			case "esc":
				if m.prevState != 0 {
					m.state = m.prevState
				} else {
					m.state = stateMain
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
				m.state = stateMain
				return m, nil
			case "enter":
				if item, ok := m.commandPalette.SelectedItem().(CommandItem); ok {
					m.state = stateMain
					if item.run != nil {
						return m, item.run(&m)
					}
				}
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
	if keyMsg, ok := msg.(tea.KeyMsg); ok && m.state != stateGridView && m.state != stateGridDetail {
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
		}
	}

	if m.state != stateGridView && m.state != stateGridDetail {
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.state == stateGridView && m.gridFiltering {
				m.gridFilter += "q"
				m.gridIndex = 0
				return m, nil
			}
			return m, tea.Quit
		case "esc":
			if m.state == stateMain {
				return m, tea.Quit
			}
			if m.state == stateGridDetail {
				m.state = stateGridView
				m.gridDetailPanel = nil
				return m, nil
			}
			if m.state == stateGridView {
				if m.gridFiltering {
					m.gridFilter = ""
					m.gridFiltering = false
					m.gridIndex = 0
					m.gridScrollOffset = 0
					return m, nil
				}
				m.state = stateMain
				return m, nil
			}
		case "?":
			if m.state == stateHelp {
				m.state = stateMain
			} else {
				m.state = stateHelp
			}
		case "g":
			if m.state == stateGridView && m.gridFiltering {
				m.gridFilter += "g"
				m.gridIndex = 0
				return m, nil
			}
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
			if m.state == stateGridView {
				if m.gridIndex == -1 {
					if m.globalMode {
						repos := extractUniqueRepos(m.globalWorktrees)
						if len(repos) == 0 {
							m.toast = &toast{message: "No repositories found", kind: toastError, expiresAt: time.Now().Add(toastDuration)}
							return m, toastExpireCmd()
						}
						m.availableRepos = repos
						items := make([]list.Item, len(repos))
						for i, r := range repos {
							items[i] = listItem{title: r.name, desc: r.root, kind: kindHeader}
						}
						m.menu.SetItems(items)
						m.menu.Select(0)
						m.state = stateSelectRepo
						return m, nil
					}
					m.state = stateCreateName
					m.input.SetValue("")
					return m, m.input.Focus()
				}
				if m.gridIndex == -2 {
					m.state = stateMain
					return m, nil
				}
				var panel *gridPanel
				if m.gridInAvailable && m.gridAvailIdx < len(m.gridAvailable) {
					p := m.gridAvailable[m.gridAvailIdx]
					panel = &p
				} else {
					filteredPanels := m.getFilteredGridPanels()
					if m.gridIndex >= 0 && m.gridIndex < len(filteredPanels) {
						p := filteredPanels[m.gridIndex]
						panel = &p
					}
				}
				if panel != nil {
					m.gridDetailPanel = panel
					m.gridDetailIdx = 0
					m.state = stateGridDetail
				}
				return m, nil
			}
			if m.state == stateGridDetail && m.gridDetailPanel != nil {
				panel := m.gridDetailPanel
				backIdx := 1
				if panel.hasSession {
					backIdx = 2
				}
				if panel.isOrphan {
					backIdx = 3
				}
				if m.gridDetailIdx == backIdx {
					m.state = stateGridView
					m.gridDetailPanel = nil
					return m, nil
				}
				switch m.gridDetailIdx {
				case 0:
					if panel.hasSession {
						m.jumpTarget = &JumpTarget{SessionName: panel.sessionName, Path: panel.path}
					} else {
						sessionName := panel.sessionName
						if sessionName == "" {
							sessionName = filepath.Base(panel.name)
						}
						m.jumpTarget = &JumpTarget{SessionName: sessionName, Path: panel.path, Create: true}
					}
					return m, tea.Quit
				case 1:
					if panel.isOrphan {
						m.pending = panel.sessionName
						m.prevState = stateGridView
						m.nextBranchState = stateOrphanBranch
						return m, branchesCmd(m.svc)
					} else if panel.hasSession {
						return m, killSessionCmd(m.svc, panel.sessionName)
					}
				case 2:
					if panel.isOrphan {
						return m, killSessionDirectCmd(m.tmux, panel.sessionName)
					}
				}
				return m, nil
			}
			if sel, ok := m.list.SelectedItem().(listItem); ok {
				switch sel.kind {
				case kindCreate:
					if m.globalMode {
						repos := extractUniqueRepos(m.globalWorktrees)
						if len(repos) == 0 {
							m.toast = &toast{message: "No repositories found in search paths", kind: toastError, expiresAt: time.Now().Add(toastDuration)}
							return m, toastExpireCmd()
						}
						m.availableRepos = repos
						items := make([]list.Item, len(repos))
						for i, r := range repos {
							items[i] = listItem{title: r.name, desc: r.root, kind: kindHeader}
						}
						m.menu.SetItems(items)
						m.menu.Select(0)
						m.state = stateSelectRepo
						return m, nil
					}
					m.state = stateCreateName
					m.input.SetValue("")
					return m, m.input.Focus()
				case kindGridView:
					m.buildGridPanels()
					if len(m.gridPanels) == 0 && len(m.gridAvailable) == 0 {
						m.toast = &toast{message: "No sessions or worktrees", kind: toastWarning, expiresAt: time.Now().Add(toastDuration)}
						return m, toastExpireCmd()
					}
					m.state = stateGridView
					m.gridIndex = 0
					m.gridFilter = ""
					m.gridFiltering = false
					m.gridScrollOffset = 0
					if len(m.gridPanels) == 0 && len(m.gridAvailable) > 0 {
						m.gridInAvailable = true
						m.gridAvailIdx = 0
					}
					return m, m.loadGridContentCmd()
				case kindWorktree:
					wt := sel.data.(workspace.WorktreeState)
					m.pendingWT = &wt
					m.menu.SetItems(actionMenuItems(wt.HasSession))
					m.menu.Select(0)
					m.state = stateActionMenu
				case kindOrphan:
					m.pending = sel.title
					m.prevState = stateMain
					if m.globalMode {
						m.menu.SetItems(globalOrphanMenuItems())
					} else {
						m.menu.SetItems(orphanMenuItems())
					}
					m.menu.Select(0)
					m.state = stateOrphanMenu
				case kindRecent:
					r := sel.data.(recent.Entry)
					if !m.tmux.HasSession(r.SessionName) {
						if err := m.tmux.NewSession(r.SessionName, r.Path); err != nil {
							m.toast = &toast{message: "Failed to create session: " + err.Error(), kind: toastError, expiresAt: time.Now().Add(toastDuration)}
							return m, toastExpireCmd()
						}
					}
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
			if m.state == stateGridDetail && m.gridDetailPanel != nil {
				maxIdx := 1
				if m.gridDetailPanel.hasSession {
					maxIdx = 2
				}
				if m.gridDetailPanel.isOrphan {
					maxIdx = 3
				}
				m.gridDetailIdx++
				if m.gridDetailIdx > maxIdx {
					m.gridDetailIdx = 0
				}
				return m, nil
			}
			if m.state == stateGridView {
				filteredLen := len(m.getFilteredGridPanels())
				if m.gridIndex == -1 {
					m.gridIndex = -2
					return m, nil
				}
				if m.gridIndex == -2 {
					if filteredLen > 0 {
						m.gridIndex = 0
					} else if len(m.gridAvailable) > 0 {
						m.gridInAvailable = true
						m.gridAvailIdx = 0
					}
					return m, nil
				}
				if m.gridInAvailable {
					if m.gridAvailIdx < len(m.gridAvailable)-1 {
						m.gridAvailIdx++
					} else {
						m.gridInAvailable = false
						m.gridIndex = -1
					}
				} else {
					if m.gridIndex < filteredLen-1 {
						m.gridIndex++
					} else if len(m.gridAvailable) > 0 {
						m.gridInAvailable = true
						m.gridAvailIdx = 0
					} else {
						m.gridIndex = -1
					}
				}
				return m, nil
			}
			if sel, ok := m.list.SelectedItem().(listItem); ok && sel.kind == kindWorktree {
				wt := sel.data.(workspace.WorktreeState)
				m.pendingWT = &wt
				m.menu.SetItems(actionMenuItems(wt.HasSession))
				m.menu.Select(0)
				m.state = stateActionMenu
			}
		case "shift+tab":
			if m.state == stateGridView {
				filteredLen := len(m.getFilteredGridPanels())
				if m.gridIndex == -1 {
					if len(m.gridAvailable) > 0 {
						m.gridInAvailable = true
						m.gridAvailIdx = len(m.gridAvailable) - 1
					} else if filteredLen > 0 {
						m.gridIndex = filteredLen - 1
					} else {
						m.gridIndex = -2
					}
					return m, nil
				}
				if m.gridIndex == -2 {
					m.gridIndex = -1
					return m, nil
				}
				if m.gridInAvailable {
					if m.gridAvailIdx > 0 {
						m.gridAvailIdx--
					} else if filteredLen > 0 {
						m.gridInAvailable = false
						m.gridIndex = filteredLen - 1
					} else {
						m.gridInAvailable = false
						m.gridIndex = -2
					}
					return m, nil
				}
				if m.gridIndex == 0 {
					m.gridIndex = -2
					return m, nil
				}
				if m.gridIndex > 0 {
					m.gridIndex--
				}
				return m, nil
			}
		case "ctrl+d":
			if m.globalMode || m.svc == nil {
				return m, nil
			}
			if sel, ok := m.list.SelectedItem().(listItem); ok && sel.kind == kindWorktree {
				wt := sel.data.(workspace.WorktreeState)
				if m.svc.Git != nil && wt.Worktree.Path == m.svc.Git.RepoRoot {
					m.toast = &toast{message: "Cannot delete current worktree", kind: toastError, expiresAt: time.Now().Add(toastDuration)}
					return m, toastExpireCmd()
				}
				cmds = append(cmds, deleteWorktreeCmd(m.svc, wt.Worktree.Path))
			}
		case "ctrl+p":
			return m, m.openCommandPalette()
		case "/":
			if m.state == stateGridView && !m.gridFiltering {
				m.gridFiltering = true
				m.gridFilter = ""
				m.gridInAvailable = false
				m.gridIndex = 0
				return m, nil
			}
		case "ctrl+g":
			if m.state == stateMain {
				m.buildGridPanels()
				if len(m.gridPanels) == 0 && len(m.gridAvailable) == 0 {
					m.toast = &toast{message: "No sessions or worktrees", kind: toastWarning, expiresAt: time.Now().Add(toastDuration)}
					return m, toastExpireCmd()
				}
				m.state = stateGridView
				m.gridIndex = 0
				m.gridFilter = ""
				m.gridFiltering = false
				m.gridScrollOffset = 0
				if len(m.gridPanels) == 0 && len(m.gridAvailable) > 0 {
					m.gridInAvailable = true
					m.gridAvailIdx = 0
				}
				return m, m.loadGridContentCmd()
			} else if m.state == stateGridView {
				m.state = stateMain
				return m, nil
			}
		case "left", "h":
			if m.state == stateGridView {
				if m.gridIndex == -1 {
					return m, nil
				}
				if m.gridIndex == -2 {
					m.gridIndex = -1
					return m, nil
				}
				if m.gridInAvailable {
					if m.gridAvailIdx > 0 {
						m.gridAvailIdx--
					} else {
						filteredLen := len(m.getFilteredGridPanels())
						if filteredLen > 0 {
							m.gridInAvailable = false
							m.gridIndex = filteredLen - 1
						} else {
							m.gridInAvailable = false
							m.gridIndex = -2
						}
					}
					return m, nil
				}
				if m.gridIndex == 0 {
					m.gridIndex = -2
					return m, nil
				}
				if m.gridIndex > 0 {
					m.gridIndex--
				}
				return m, nil
			}
		case "right", "l":
			if m.state == stateGridView {
				if m.gridIndex == -1 {
					m.gridIndex = -2
					return m, nil
				}
				if m.gridIndex == -2 {
					filteredLen := len(m.getFilteredGridPanels())
					if filteredLen > 0 {
						m.gridIndex = 0
					} else if len(m.gridAvailable) > 0 {
						m.gridInAvailable = true
						m.gridAvailIdx = 0
					}
					return m, nil
				}
				if m.gridInAvailable {
					if m.gridAvailIdx < len(m.gridAvailable)-1 {
						m.gridAvailIdx++
					}
				} else {
					filteredLen := len(m.getFilteredGridPanels())
					if m.gridIndex < filteredLen-1 {
						m.gridIndex++
					} else if len(m.gridAvailable) > 0 {
						m.gridInAvailable = true
						m.gridAvailIdx = 0
					}
				}
				return m, nil
			}
		case "up", "k":
			if m.state == stateGridDetail {
				if m.gridDetailIdx > 0 {
					m.gridDetailIdx--
				}
				return m, nil
			}
			if m.state == stateGridView {
				if m.gridInAvailable {
					if m.gridAvailIdx >= m.gridCols {
						m.gridAvailIdx -= m.gridCols
					} else {
						filteredPanels := m.getFilteredGridPanels()
						if len(filteredPanels) > 0 {
							m.gridInAvailable = false
							m.gridIndex = len(filteredPanels) - 1
						} else {
							m.gridInAvailable = false
							m.gridIndex = -2
						}
					}
				} else if m.gridIndex == -1 {
					return m, nil
				} else if m.gridIndex == -2 {
					m.gridIndex = -1
				} else {
					filteredPanels := m.getFilteredGridPanels()
					if m.gridIndex >= len(filteredPanels) || len(filteredPanels) == 0 {
						m.gridIndex = -2
						return m, nil
					}
					var sessionCount, recentCount int
					for _, p := range filteredPanels {
						if !p.isOrphan && !p.isRecent {
							sessionCount++
						} else if p.isRecent {
							recentCount++
						}
					}
					currentPanel := filteredPanels[m.gridIndex]
					if currentPanel.isOrphan {
						firstOrphanIdx := sessionCount + recentCount
						localIdx := m.gridIndex - firstOrphanIdx
						if localIdx >= m.gridCols {
							m.gridIndex -= m.gridCols
						} else if firstOrphanIdx > 0 {
							m.gridIndex = firstOrphanIdx - 1
						} else {
							m.gridIndex = -2
						}
					} else if currentPanel.isRecent {
						localIdx := m.gridIndex - sessionCount
						if localIdx >= m.gridCols {
							m.gridIndex -= m.gridCols
						} else if sessionCount > 0 {
							m.gridIndex = sessionCount - 1
						} else {
							m.gridIndex = -2
						}
					} else {
						if m.gridIndex >= m.gridCols {
							m.gridIndex -= m.gridCols
						} else {
							m.gridIndex = -2
						}
					}
				}
				return m, nil
			}
		case "down", "j":
			if m.state == stateGridDetail && m.gridDetailPanel != nil {
				maxIdx := 1
				if m.gridDetailPanel.hasSession {
					maxIdx = 2
				}
				if m.gridDetailPanel.isOrphan {
					maxIdx = 3
				}
				if m.gridDetailIdx < maxIdx {
					m.gridDetailIdx++
				}
				return m, nil
			}
			if m.state == stateGridView {
				if m.gridInAvailable {
					if m.gridAvailIdx+m.gridCols < len(m.gridAvailable) {
						m.gridAvailIdx += m.gridCols
					} else {
						nextRowStart := ((m.gridAvailIdx / m.gridCols) + 1) * m.gridCols
						if nextRowStart < len(m.gridAvailable) {
							m.gridAvailIdx = nextRowStart
						}
					}
					return m, nil
				}
				if m.gridIndex == -1 {
					m.gridIndex = -2
					return m, nil
				}
				if m.gridIndex == -2 {
					filteredPanels := m.getFilteredGridPanels()
					if len(filteredPanels) > 0 {
						m.gridIndex = 0
					} else if len(m.gridAvailable) > 0 {
						m.gridInAvailable = true
						m.gridAvailIdx = 0
					}
					return m, nil
				}
				filteredPanels := m.getFilteredGridPanels()
				if m.gridIndex >= len(filteredPanels) || len(filteredPanels) == 0 {
					return m, nil
				}
				var sessionCount, recentCount, orphanCount int
				for _, p := range filteredPanels {
					if !p.isOrphan && !p.isRecent {
						sessionCount++
					} else if p.isRecent {
						recentCount++
					} else {
						orphanCount++
					}
				}
				currentPanel := filteredPanels[m.gridIndex]
				if !currentPanel.isOrphan && !currentPanel.isRecent {
					localIdx := m.gridIndex
					if localIdx+m.gridCols < sessionCount {
						m.gridIndex += m.gridCols
					} else if recentCount > 0 {
						m.gridIndex = sessionCount
					} else if orphanCount > 0 {
						m.gridIndex = sessionCount + recentCount
					} else if len(m.gridAvailable) > 0 {
						m.gridInAvailable = true
						m.gridAvailIdx = 0
					}
				} else if currentPanel.isRecent {
					localIdx := m.gridIndex - sessionCount
					if localIdx+m.gridCols < recentCount {
						m.gridIndex += m.gridCols
					} else if orphanCount > 0 {
						m.gridIndex = sessionCount + recentCount
					} else if len(m.gridAvailable) > 0 {
						m.gridInAvailable = true
						m.gridAvailIdx = 0
					}
				} else {
					firstOrphanIdx := sessionCount + recentCount
					localIdx := m.gridIndex - firstOrphanIdx
					if localIdx+m.gridCols < orphanCount {
						m.gridIndex += m.gridCols
					} else if len(m.gridAvailable) > 0 {
						m.gridInAvailable = true
						m.gridAvailIdx = 0
					}
				}
				return m, nil
			}
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			if m.state == stateGridView {
				if m.gridFiltering {
					m.gridFilter += msg.String()
					m.gridIndex = 0
					return m, nil
				}
				filteredPanels := m.getFilteredGridPanels()
				idx := int(msg.String()[0] - '1')
				if idx >= 0 && idx < len(filteredPanels) {
					panel := filteredPanels[idx]
					if panel.isOrphan {
						m.pending = panel.sessionName
						m.prevState = stateGridView
						if m.globalMode {
							m.menu.SetItems(globalOrphanMenuItems())
						} else {
							m.menu.SetItems(orphanMenuItems())
						}
						m.menu.Select(0)
						m.state = stateOrphanMenu
						return m, nil
					}
					if panel.hasSession {
						m.jumpTarget = &JumpTarget{SessionName: panel.sessionName, Path: panel.path}
						return m, tea.Quit
					}
				}
				return m, nil
			}
		case "backspace":
			if m.state == stateGridView && m.gridFiltering {
				if len(m.gridFilter) > 0 {
					m.gridFilter = m.gridFilter[:len(m.gridFilter)-1]
					m.gridIndex = 0
				}
				return m, nil
			}
		case "r":
			if m.state == stateGridView && m.gridFiltering {
				m.gridFilter += "r"
				m.gridIndex = 0
				return m, nil
			}
			m.toast = &toast{message: "Refreshing...", kind: toastInfo, expiresAt: time.Now().Add(toastDuration)}
			m.refreshInFlight++
			if m.globalMode {
				return m, tea.Batch(loadGlobalDataCmd(m.cfg, m.tmux), toastExpireCmd())
			}
			if m.svc == nil {
				m.refreshInFlight--
				return m, toastExpireCmd()
			}
			return m, tea.Batch(loadDataCmd(m.svc), toastExpireCmd())
		default:
			if m.state == stateGridView && m.gridFiltering {
				key := msg.String()
				if len(key) == 1 {
					ch := key[0]
					if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
						(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' {
						m.gridFilter += strings.ToLower(key)
						m.gridIndex = 0
						return m, nil
					}
				}
			}
		}
	case tea.MouseMsg:
		// not used
	}

	if m.state == stateGridView || m.state == stateGridDetail {
		m.updateGridScroll()
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

func (m *model) getFilteredGridPanels() []gridPanel {
	if m.gridFilter == "" {
		return m.gridPanels
	}
	filtered := []gridPanel{}
	filterLower := strings.ToLower(m.gridFilter)
	for _, p := range m.gridPanels {
		if strings.Contains(strings.ToLower(p.name), filterLower) ||
			strings.Contains(strings.ToLower(p.branch), filterLower) ||
			strings.Contains(strings.ToLower(p.sessionName), filterLower) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func (m *model) updateGridScroll() {
	gridWidth := m.width - 4
	if gridWidth < 32 {
		gridWidth = 32
	}
	minPanelWidth := 32
	m.gridCols = gridWidth / minPanelWidth
	if m.gridCols < 1 {
		m.gridCols = 1
	}
	if m.gridCols > 4 {
		m.gridCols = 4
	}

	filteredPanels := m.getFilteredGridPanels()
	var sessionPanels, recentPanels, orphanPanels []gridPanel
	for _, p := range filteredPanels {
		if p.isOrphan {
			orphanPanels = append(orphanPanels, p)
		} else if p.isRecent {
			recentPanels = append(recentPanels, p)
		} else {
			sessionPanels = append(sessionPanels, p)
		}
	}

	headerHeight := 3
	footerHeight := 3
	availableHeight := m.height - headerHeight - footerHeight
	if availableHeight < 1 {
		availableHeight = 1
	}

	panelLines := 6
	actionItemsLines := 5
	sectionHeaderLines := 3

	sessionsLines := 0
	if len(sessionPanels) > 0 {
		sessionsLines = sectionHeaderLines + ((len(sessionPanels)+m.gridCols-1)/m.gridCols)*panelLines
	}
	recentLines := 0
	if len(recentPanels) > 0 {
		recentLines = sectionHeaderLines + ((len(recentPanels)+m.gridCols-1)/m.gridCols)*panelLines
	}
	orphansLines := 0
	if len(orphanPanels) > 0 {
		orphansLines = sectionHeaderLines + ((len(orphanPanels)+m.gridCols-1)/m.gridCols)*panelLines
	}
	noMatchLines := 0
	if len(filteredPanels) == 0 && m.gridFiltering {
		noMatchLines = 1
	}

	var selectedLine int
	if m.gridInAvailable {
		availRowIdx := m.gridAvailIdx / m.gridCols
		selectedLine = actionItemsLines + sessionsLines + recentLines + orphansLines + noMatchLines + sectionHeaderLines + availRowIdx*panelLines
	} else if m.gridIndex < 0 {
		selectedLine = 0
	} else {
		if m.gridIndex < len(sessionPanels) {
			rowIdx := m.gridIndex / m.gridCols
			selectedLine = actionItemsLines + sectionHeaderLines + rowIdx*panelLines
		} else if m.gridIndex < len(sessionPanels)+len(recentPanels) {
			recentLocalIdx := m.gridIndex - len(sessionPanels)
			rowIdx := recentLocalIdx / m.gridCols
			selectedLine = actionItemsLines + sessionsLines + sectionHeaderLines + rowIdx*panelLines
		} else {
			orphanLocalIdx := m.gridIndex - len(sessionPanels) - len(recentPanels)
			rowIdx := orphanLocalIdx / m.gridCols
			selectedLine = actionItemsLines + sessionsLines + recentLines + sectionHeaderLines + rowIdx*panelLines
		}
	}

	if selectedLine < m.gridScrollOffset {
		m.gridScrollOffset = selectedLine
	} else if selectedLine+panelLines > m.gridScrollOffset+availableHeight {
		m.gridScrollOffset = selectedLine + panelLines - availableHeight
	}
	if m.gridScrollOffset < 0 {
		m.gridScrollOffset = 0
	}
}

func (m *model) buildGridPanels() {
	m.gridPanels = []gridPanel{}
	m.gridAvailable = []gridPanel{}
	m.gridInAvailable = false
	m.gridAvailIdx = 0
	
	if m.globalMode {
		for _, wt := range m.globalWorktrees {
			sessionName := wt.Worktree.Name
			if m.tmux.HasSession(sessionName) {
				panel := gridPanel{
					name:        wt.RepoName + "/" + wt.Worktree.Name,
					sessionName: sessionName,
					path:        wt.Worktree.Path,
					branch:      wt.Worktree.Branch,
					hasSession:  true,
				}
				if info, err := m.tmux.SessionInfo(sessionName); err == nil && info != nil {
					panel.windows = info.Windows
					panel.panes = info.Panes
				}
				m.gridPanels = append(m.gridPanels, panel)
			} else {
				m.gridAvailable = append(m.gridAvailable, gridPanel{
					name:       wt.RepoName + "/" + wt.Worktree.Name,
					path:       wt.Worktree.Path,
					branch:     wt.Worktree.Branch,
					hasSession: false,
				})
			}
		}
	} else {
		for _, st := range m.states {
			if st.HasSession {
				panel := gridPanel{
					name:        st.Worktree.Name,
					sessionName: st.SessionName,
					path:        st.Worktree.Path,
					branch:      st.Worktree.Branch,
					hasSession:  true,
				}
				if st.Status != nil {
					panel.modified = st.Status.Modified
					panel.staged = st.Status.Staged
				}
				if st.SessionInfo != nil {
					panel.windows = st.SessionInfo.Windows
					panel.panes = st.SessionInfo.Panes
				}
				panel.processes = st.Processes
				m.gridPanels = append(m.gridPanels, panel)
			} else {
				panel := gridPanel{
					name:       st.Worktree.Name,
					path:       st.Worktree.Path,
					branch:     st.Worktree.Branch,
					hasSession: false,
				}
				if st.Status != nil {
					panel.modified = st.Status.Modified
					panel.staged = st.Status.Staged
				}
				m.gridAvailable = append(m.gridAvailable, panel)
			}
		}
	}

	if !m.globalMode {
		for _, r := range m.recentEntries {
			sessionName := r.SessionName
			if sessionName == "" {
				sessionName = r.Worktree
			}
			panel := gridPanel{
				name:        r.RepoName + "/" + r.Worktree,
				sessionName: sessionName,
				path:        r.Path,
				hasSession:  m.tmux.HasSession(sessionName),
				isRecent:    true,
			}
			if panel.hasSession {
				if info, err := m.tmux.SessionInfo(sessionName); err == nil && info != nil {
					panel.windows = info.Windows
					panel.panes = info.Panes
				}
			}
			m.gridPanels = append(m.gridPanels, panel)
		}
	}

	sortedOrphans := make([]string, len(m.orphans))
	copy(sortedOrphans, m.orphans)
	sort.Strings(sortedOrphans)
	for _, o := range sortedOrphans {
		panel := gridPanel{
			name:        o,
			sessionName: o,
			hasSession:  true,
			isOrphan:    true,
		}
		if info, err := m.tmux.SessionInfo(o); err == nil && info != nil {
			panel.windows = info.Windows
			panel.panes = info.Panes
		}
		m.gridPanels = append(m.gridPanels, panel)
	}
	
	gridWidth := m.width - 4
	if gridWidth < 32 {
		gridWidth = 32
	}
	minPanelWidth := 32
	m.gridCols = gridWidth / minPanelWidth
	if m.gridCols < 1 {
		m.gridCols = 1
	}
	if m.gridCols > 4 {
		m.gridCols = 4
	}
}

type gridContentMsg struct {
	contents map[string]string
}

func (m *model) loadGridContentCmd() tea.Cmd {
	panels := m.gridPanels
	tmux := m.tmux
	return func() tea.Msg {
		contents := make(map[string]string)
		for _, p := range panels {
			if p.hasSession {
				content, err := tmux.CapturePane(p.sessionName, 8)
				if err == nil {
					contents[p.sessionName] = content
				}
			}
		}
		return gridContentMsg{contents: contents}
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
	m.state = stateCommandPalette
	return nil
}

func (m *model) commandPaletteItems() []CommandItem {
	items := []CommandItem{
		{label: "Create worktree", desc: "Create new worktree from branch", run: func(m *model) tea.Cmd {
			if m.globalMode {
				repos := extractUniqueRepos(m.globalWorktrees)
				if len(repos) == 0 {
					m.toast = &toast{message: "No repositories found", kind: toastError, expiresAt: time.Now().Add(toastDuration)}
					return toastExpireCmd()
				}
				m.availableRepos = repos
				items := make([]list.Item, len(repos))
				for i, r := range repos {
					items[i] = listItem{title: r.name, desc: r.root, kind: kindHeader}
				}
				m.menu.SetItems(items)
				m.menu.Select(0)
				m.state = stateSelectRepo
				return nil
			}
			m.state = stateCreateName
			m.input.SetValue("")
			return m.input.Focus()
		}},
		{label: "Toggle global mode", desc: "Switch between repo and global view", run: func(m *model) tea.Cmd {
			m.globalMode = !m.globalMode
			m.loading = true
			if m.globalMode {
				return tea.Batch(m.spinner.Tick, loadGlobalDataCmd(m.cfg, m.tmux))
			}
			if m.inGitRepo {
				return tea.Batch(m.spinner.Tick, loadDataCmd(m.svc))
			}
			m.globalMode = true
			return tea.Batch(m.spinner.Tick, loadGlobalDataCmd(m.cfg, m.tmux))
		}},
		{label: "Refresh", desc: "Reload worktree and session data", run: func(m *model) tea.Cmd {
			m.toast = &toast{message: "Refreshing...", kind: toastInfo, expiresAt: time.Now().Add(toastDuration)}
			m.refreshInFlight++
			if m.globalMode {
				return tea.Batch(loadGlobalDataCmd(m.cfg, m.tmux), toastExpireCmd())
			}
			if m.svc == nil {
				m.refreshInFlight--
				return toastExpireCmd()
			}
			return tea.Batch(loadDataCmd(m.svc), toastExpireCmd())
		}},
		{label: "Show help", desc: "Display keybindings and commands", run: func(m *model) tea.Cmd {
			m.state = stateHelp
			return nil
		}},
		{label: "Quit", desc: "Exit treemux", run: func(m *model) tea.Cmd {
			return tea.Quit
		}},
	}

	if sel, ok := m.list.SelectedItem().(listItem); ok {
		switch sel.kind {
		case kindWorktree:
			if m.svc == nil {
				break
			}
			wt := sel.data.(workspace.WorktreeState)
			items = append(items,
				CommandItem{label: "Jump to worktree", desc: "Switch to selected worktree session", run: func(m *model) tea.Cmd {
					if m.svc == nil {
						return nil
					}
					sessionName := m.svc.SessionName(wt.Worktree.Path)
					if !m.svc.Tmux.HasSession(sessionName) {
						_ = m.svc.Tmux.NewSession(sessionName, wt.Worktree.Path)
					}
					if m.recentStore != nil && m.svc.Git != nil {
						m.recentStore.Add(m.svc.Git.RepoRoot, wt.Worktree.Name, sessionName, wt.Worktree.Path)
						_ = m.recentStore.Save()
					}
					m.jumpTarget = &JumpTarget{SessionName: sessionName, Path: wt.Worktree.Path}
					return tea.Quit
				}},
				CommandItem{label: "Delete worktree", desc: "Remove worktree and kill session", run: func(m *model) tea.Cmd {
					if m.svc == nil || m.svc.Git == nil {
						return nil
					}
					if wt.Worktree.Path == m.svc.Git.RepoRoot {
						m.toast = &toast{message: "Cannot delete current worktree", kind: toastError, expiresAt: time.Now().Add(toastDuration)}
						return toastExpireCmd()
					}
					return deleteWorktreeCmd(m.svc, wt.Worktree.Path)
				}},
			)
			if wt.HasSession {
				items = append(items, CommandItem{label: "Kill session", desc: "Kill tmux session only", run: func(m *model) tea.Cmd {
					if m.svc == nil {
						return nil
					}
					return killSessionCmd(m.svc, wt.SessionName)
				}})
			}
		case kindGlobal:
			wt := sel.data.(scanner.RepoWorktree)
			sessionName := wt.Worktree.Name
			items = append(items,
				CommandItem{label: "Jump to worktree", desc: "Switch to selected worktree session", run: func(m *model) tea.Cmd {
					if !m.tmux.HasSession(sessionName) {
						_ = m.tmux.NewSession(sessionName, wt.Worktree.Path)
					}
					m.jumpTarget = &JumpTarget{SessionName: sessionName, Path: wt.Worktree.Path}
					return tea.Quit
				}},
			)
		case kindOrphan:
			sessionName := sel.title
			items = append(items, CommandItem{label: "Kill orphan session", desc: "Kill this orphaned session", run: func(m *model) tea.Cmd {
				if m.globalMode {
					return killSessionDirectCmd(m.tmux, sessionName)
				}
				if m.svc == nil {
					return nil
				}
				return killSessionCmd(m.svc, sessionName)
			}})
		}
	}

	return items
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
	case stateSelectRepo:
		return renderMenu("Select repository", &m.menu)
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
	case stateCommandPalette:
		return renderCommandPalette(&m.commandPalette, m.width, m.height)
	case stateGridView:
		return m.renderGridView()
	case stateGridDetail:
		return m.renderGridDetail()
	}

	left := listFrameStyle.Render(m.list.View())

	previewContent := m.renderPreviewWithTerminal()
	right := previewFrameStyle.Render(previewContent)

	if m.toast != nil && !m.toast.expired() {
		styles := toastStyles{
			success: successStyle.Copy().Bold(true),
			error:   errorStyle.Copy().Bold(true),
			warning: warnStyle.Copy().Bold(true),
			info:    sectionStyle.Copy().Bold(true),
		}
		right = m.toast.render(styles) + "\n\n" + right
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

	repoIndicator := ""
	if m.globalMode {
		repoIndicator = warnStyle.Render("GLOBAL")
	} else if m.svc != nil && m.svc.Git != nil {
		repoName := filepath.Base(m.svc.Git.RepoRoot)
		repoIndicator = sectionStyle.Render(repoName)
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
	colors := []lipgloss.Color{"#f5c2e7", "#cba6f7", "#b4befe", "#89b4fa", "#94e2d5"}
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

	toggleHint := keyStyle.Render("g") + dimStyle.Render(" global  ")
	if m.globalMode {
		toggleHint = keyStyle.Render("g") + dimStyle.Render(" repo  ")
	}

	sep := lipgloss.NewStyle().Foreground(overlayColor).Render(" ┃ ")
	footerContent := keyStyle.Render("enter") + dimStyle.Render(" select  ") +
		keyStyle.Render("/") + dimStyle.Render(" filter") +
		sep +
		keyStyle.Render("ctrl+g") + dimStyle.Render(" grid view  ") +
		keyStyle.Render("ctrl+p") + dimStyle.Render(" cmd  ") +
		toggleHint +
		sep +
		keyStyle.Render("?") + dimStyle.Render(" help  ") +
		keyStyle.Render("q") + dimStyle.Render(" quit")
	
	footer := lipgloss.NewStyle().
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(overlayColor).
		Padding(0, 2).
		Foreground(subTextColor).
		Render(footerContent)

	sepHeight := m.height - 7
	if sepHeight < 1 {
		sepHeight = 1
	}
	sepColors := []lipgloss.Color{accent, accent2, teal, accent2, accent}
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

func buildGlobalItems(worktrees []scanner.RepoWorktree, orphans []string) []list.Item {
	items := []list.Item{}
	items = append(items, listItem{
		title: "+ Create new worktree ...",
		kind:  kindCreate,
	})
	if len(orphans) > 0 || len(worktrees) > 0 {
		items = append(items, listItem{
			title: "Grid View",
			kind:  kindGridView,
		})
	}

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

func (m *model) renderGridView() string {
	if len(m.gridPanels) == 0 && len(m.gridAvailable) == 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			dimStyle.Render("No sessions or worktrees"))
	}

	gridWidth := m.width - 4
	if gridWidth < 32 {
		gridWidth = 32
	}

	minPanelWidth := 32
	m.gridCols = gridWidth / minPanelWidth
	if m.gridCols < 1 {
		m.gridCols = 1
	}
	if m.gridCols > 4 {
		m.gridCols = 4
	}
	panelWidth := gridWidth / m.gridCols
	innerWidth := panelWidth - 4

	t1 := lipgloss.NewStyle().Foreground(lipgloss.Color("#f5c2e7")).Bold(true)
	t2 := lipgloss.NewStyle().Foreground(lipgloss.Color("#cba6f7")).Bold(true)
	t3 := lipgloss.NewStyle().Foreground(lipgloss.Color("#89b4fa")).Bold(true)
	title := lipgloss.NewStyle().Foreground(successColor).Bold(true).Render("▲ ") +
		t1.Render("tree") + t2.Render("mu") + t3.Render("x")

	if m.gridFiltering {
		filterStyle := lipgloss.NewStyle().
			Foreground(baseBg).
			Background(teal).
			Bold(true).
			Padding(0, 1)
		title += "  " + filterStyle.Render("/"+m.gridFilter+"_")
	}

	hint := dimStyle.Render("/") + " " + subTextStyle.Render("filter") + "  " +
		dimStyle.Render("1-9") + " " + subTextStyle.Render("quick jump") + "  " +
		dimStyle.Render("enter") + " " + subTextStyle.Render("open") + "  " +
		dimStyle.Render("ctrl+g") + " " + subTextStyle.Render("list view") + "  " +
		dimStyle.Render("esc") + " " + subTextStyle.Render("back")

	header := lipgloss.NewStyle().Padding(1, 2).Render(title)

	filteredPanels := m.getFilteredGridPanels()

	if len(filteredPanels) == 0 && len(m.gridAvailable) == 0 {
		noResults := lipgloss.NewStyle().Foreground(dimColor).Render("No matching sessions")
		return lipgloss.JoinVertical(lipgloss.Left, header, "\n"+noResults)
	}

	var sessionPanels, recentPanels, orphanPanels []gridPanel
	sessionIndices := make(map[int]int)
	recentIndices := make(map[int]int)
	orphanIndices := make(map[int]int)
	for i, p := range filteredPanels {
		if p.isOrphan {
			orphanIndices[len(orphanPanels)] = i
			orphanPanels = append(orphanPanels, p)
		} else if p.isRecent {
			recentIndices[len(recentPanels)] = i
			recentPanels = append(recentPanels, p)
		} else {
			sessionIndices[len(sessionPanels)] = i
			sessionPanels = append(sessionPanels, p)
		}
	}

	renderSectionHeader := func(text string) string {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6c7086")).
			MarginTop(1).
			MarginBottom(1).
			Render("── " + text + " " + strings.Repeat("─", gridWidth-len(text)-5))
	}

	renderPanel := func(panel gridPanel, globalIdx int, isSelected bool, isActive bool) string {
		borderColor := lipgloss.Color("#313244")
		titleBg := lipgloss.Color("#1e1e2e")
		if isSelected {
			borderColor = successColor
			titleBg = lipgloss.Color("#313244")
		}

		trafficStyle := lipgloss.NewStyle()
		if isActive {
			trafficStyle = trafficStyle.Foreground(lipgloss.Color("#a6e3a1"))
		} else {
			trafficStyle = trafficStyle.Foreground(lipgloss.Color("#45475a"))
		}
		traffic := trafficStyle.Render("●●●")

		displayName := panel.name
		maxNameLen := innerWidth - 8
		if maxNameLen < 5 {
			maxNameLen = 5
		}
		if len(displayName) > maxNameLen {
			displayName = displayName[:maxNameLen-1] + "…"
		}

		nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#cdd6f4"))
		if isSelected {
			nameStyle = nameStyle.Foreground(successColor).Bold(true)
		} else if !isActive {
			nameStyle = nameStyle.Foreground(lipgloss.Color("#6c7086"))
		}

		titleContent := traffic + " " + nameStyle.Render(displayName)
		titleBar := lipgloss.NewStyle().
			Width(innerWidth).
			Background(titleBg).
			Padding(0, 1).
			Render(titleContent)

		line1 := ""
		if panel.branch != "" {
			branchDisplay := panel.branch
			maxBranchLen := innerWidth - 4
			if len(branchDisplay) > maxBranchLen {
				branchDisplay = branchDisplay[:maxBranchLen-1] + "…"
			}
			line1 = lipgloss.NewStyle().Foreground(lipgloss.Color("#89b4fa")).Render("⎇ " + branchDisplay)
		}

		line2 := ""
		if isActive {
			statusText := "● active"
			if panel.windows > 0 {
				statusText += fmt.Sprintf(" %dw %dp", panel.windows, panel.panes)
			}
			line2 = lipgloss.NewStyle().Foreground(lipgloss.Color("#a6e3a1")).Render(statusText)
		} else {
			line2 = lipgloss.NewStyle().Foreground(lipgloss.Color("#45475a")).Render("○ inactive")
		}

		line3 := ""
		if globalIdx < 9 {
			line3 = lipgloss.NewStyle().Foreground(lipgloss.Color("#45475a")).Render(fmt.Sprintf("[%d]", globalIdx+1))
		}

		content := lipgloss.NewStyle().
			Width(innerWidth).
			Height(3).
			Padding(0, 1).
			Render(line1 + "\n" + line2 + "\n" + line3)

		panelContent := titleBar + "\n" + content

		return lipgloss.NewStyle().
			Width(panelWidth - 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Render(panelContent)
	}

	renderPanelGrid := func(panels []gridPanel, indices map[int]int, active bool) string {
		if len(panels) == 0 {
			return ""
		}
		var rows []string
		var currentRow []string
		for localIdx, panel := range panels {
			globalIdx := indices[localIdx]
			isSelected := globalIdx == m.gridIndex && !m.gridInAvailable
			renderedPanel := renderPanel(panel, globalIdx, isSelected, active || panel.hasSession)
			currentRow = append(currentRow, renderedPanel)

			if len(currentRow) >= m.gridCols || localIdx == len(panels)-1 {
				rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, currentRow...))
				currentRow = []string{}
			}
		}
		return lipgloss.JoinVertical(lipgloss.Left, rows...)
	}

	var gridSections []string

	renderActionItem := func(icon, title, desc string, selected bool) string {
		if selected {
			titleLine := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#94e2d5")).
				Bold(true).
				Render(icon + " " + title)
			descLine := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6c7086")).
				Render("  " + desc)
			return lipgloss.NewStyle().
				Width(gridWidth).
				Background(lipgloss.Color("#313244")).
				BorderLeft(true).
				BorderStyle(lipgloss.ThickBorder()).
				BorderForeground(lipgloss.Color("#94e2d5")).
				PaddingLeft(1).
				Render(titleLine + "\n" + descLine)
		}
		titleLine := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#cdd6f4")).
			Render(icon + " " + title)
		descLine := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6c7086")).
			Render("  " + desc)
		return lipgloss.NewStyle().
			PaddingLeft(2).
			Render(titleLine + "\n" + descLine)
	}

	newWorktreeItem := renderActionItem("+", "New Worktree", "Create worktree and session", m.gridIndex == -1 && !m.gridInAvailable)
	listViewItem := renderActionItem("☰", "List View", "View all sessions", m.gridIndex == -2 && !m.gridInAvailable)
	actionItems := lipgloss.NewStyle().MarginBottom(1).Render(newWorktreeItem + "\n" + listViewItem)
	gridSections = append(gridSections, actionItems)

	if len(sessionPanels) > 0 {
		gridSections = append(gridSections, renderSectionHeader("SESSIONS"))
		gridSections = append(gridSections, renderPanelGrid(sessionPanels, sessionIndices, true))
	}
	if len(recentPanels) > 0 {
		gridSections = append(gridSections, renderSectionHeader("RECENT"))
		gridSections = append(gridSections, renderPanelGrid(recentPanels, recentIndices, true))
	}
	if len(orphanPanels) > 0 {
		gridSections = append(gridSections, renderSectionHeader("ORPHANED SESSIONS"))
		gridSections = append(gridSections, renderPanelGrid(orphanPanels, orphanIndices, true))
	}
	if len(filteredPanels) == 0 && m.gridFiltering {
		gridSections = append(gridSections, lipgloss.NewStyle().Foreground(dimColor).Render("No matching sessions"))
	}
	if len(m.gridAvailable) > 0 {
		gridSections = append(gridSections, renderSectionHeader("AVAILABLE WORKTREES"))

		var availRows []string
		var availRow []string
		for i, panel := range m.gridAvailable {
			isSelected := m.gridInAvailable && i == m.gridAvailIdx
			renderedPanel := renderPanel(panel, i, isSelected, false)
			availRow = append(availRow, renderedPanel)

			if len(availRow) >= m.gridCols || i == len(m.gridAvailable)-1 {
				availRows = append(availRows, lipgloss.JoinHorizontal(lipgloss.Top, availRow...))
				availRow = []string{}
			}
		}
		gridSections = append(gridSections, lipgloss.JoinVertical(lipgloss.Left, availRows...))
	}

	grid := lipgloss.JoinVertical(lipgloss.Left, gridSections...)
	headerHeight := 3
	footerHeight := 3
	availableHeight := m.height - headerHeight - footerHeight
	if availableHeight < 1 {
		availableHeight = 1
	}

	gridLines := strings.Split(grid, "\n")
	startLine := m.gridScrollOffset
	if startLine < 0 {
		startLine = 0
	}
	endLine := startLine + availableHeight
	if endLine > len(gridLines) {
		endLine = len(gridLines)
	}
	if startLine > len(gridLines) {
		startLine = len(gridLines)
	}
	visibleGrid := strings.Join(gridLines[startLine:endLine], "\n")
	body := lipgloss.NewStyle().Height(availableHeight).MarginLeft(2).Render(visibleGrid)
	
	footer := lipgloss.NewStyle().
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(overlayColor).
		Padding(0, 2).
		Render(hint)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m *model) renderGridDetail() string {
	if m.gridDetailPanel == nil {
		return ""
	}
	panel := m.gridDetailPanel

	modalWidth := 60
	if m.width < 70 {
		modalWidth = m.width - 10
	}
	if modalWidth < 40 {
		modalWidth = 40
	}

	trafficRed := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5f56")).Render("●")
	trafficYellow := lipgloss.NewStyle().Foreground(lipgloss.Color("#ffbd2e")).Render("●")
	trafficGreen := lipgloss.NewStyle().Foreground(lipgloss.Color("#27c93f")).Render("●")
	traffic := trafficRed + " " + trafficYellow + " " + trafficGreen

	titleBar := lipgloss.NewStyle().
		Width(modalWidth - 2).
		Background(lipgloss.Color("#313244")).
		Padding(0, 1).
		Render(traffic + "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#cdd6f4")).Bold(true).Render(panel.name))

	var infoLines []string

	if panel.branch != "" {
		infoLines = append(infoLines, lipgloss.NewStyle().Foreground(lipgloss.Color("#89b4fa")).Render("⎇ "+panel.branch))
	}
	if panel.path != "" {
		pathDisplay := panel.path
		maxPath := modalWidth - 8
		if len(pathDisplay) > maxPath {
			pathDisplay = "…" + pathDisplay[len(pathDisplay)-maxPath+1:]
		}
		infoLines = append(infoLines, lipgloss.NewStyle().Foreground(lipgloss.Color("#6c7086")).Render("  "+pathDisplay))
	}

	if panel.hasSession {
		sessionInfo := lipgloss.NewStyle().Foreground(lipgloss.Color("#a6e3a1")).Render("● active")
		if panel.windows > 0 {
			sessionInfo += lipgloss.NewStyle().Foreground(lipgloss.Color("#6c7086")).Render(fmt.Sprintf("  %d windows, %d panes", panel.windows, panel.panes))
		}
		infoLines = append(infoLines, sessionInfo)
	} else {
		infoLines = append(infoLines, lipgloss.NewStyle().Foreground(lipgloss.Color("#6c7086")).Render("○ no active session"))
	}

	if panel.modified > 0 || panel.staged > 0 {
		var parts []string
		if panel.modified > 0 {
			parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("#f9e2af")).Render(fmt.Sprintf("%d modified", panel.modified)))
		}
		if panel.staged > 0 {
			parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("#a6e3a1")).Render(fmt.Sprintf("%d staged", panel.staged)))
		}
		infoLines = append(infoLines, strings.Join(parts, "  "))
	}

	infoSection := lipgloss.NewStyle().
		Width(modalWidth - 4).
		Padding(1, 1).
		Render(strings.Join(infoLines, "\n"))

	divider := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#313244")).
		Render(strings.Repeat("─", modalWidth-2))

	type actionItem struct {
		label string
		key   string
	}
	var actions []actionItem
	if panel.hasSession {
		actions = append(actions, actionItem{"Jump to session", "enter"})
		if panel.isOrphan {
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
		isSelected := i == m.gridDetailIdx

		keyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#313244")).
			Background(lipgloss.Color("#6c7086")).
			Padding(0, 1)

		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6c7086"))

		if isSelected {
			keyStyle = keyStyle.Background(lipgloss.Color("#a6e3a1")).Foreground(lipgloss.Color("#1e1e2e"))
			labelStyle = labelStyle.Foreground(lipgloss.Color("#cdd6f4")).Bold(true)
		}

		line := keyStyle.Render(action.key) + " " + labelStyle.Render(action.label)
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
		BorderForeground(lipgloss.Color("#45475a")).
		Render(modalContent)

	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#6c7086")).Render("↑↓/tab navigate  enter confirm  esc back")

	modalWithHint := lipgloss.JoinVertical(lipgloss.Center, modal, "", hint)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modalWithHint)
}

func (m *model) renderGridSidebarPanel(width int, panel gridPanel) string {
	if panel.name == "" {
		return ""
	}

	sidebarStyle := lipgloss.NewStyle().
		Width(width).
		Padding(0, 1)

	nameStyle := lipgloss.NewStyle().Foreground(textColor).Bold(true)
	name := nameStyle.Render(panel.name)

	sectionHeader := func(title string) string {
		return lipgloss.NewStyle().
			Foreground(baseBg).
			Background(accent).
			Bold(true).
			Width(width - 2).
			Padding(0, 1).
			Render(title)
	}

	labelStyle := dimStyle
	valueStyle := subTextStyle

	statusLines := []string{sectionHeader("Status")}
	if panel.branch != "" {
		statusLines = append(statusLines, labelStyle.Render("Branch  ")+valueStyle.Render(panel.branch))
	}
	if panel.modified > 0 || panel.staged > 0 {
		statusParts := []string{}
		if panel.modified > 0 {
			statusParts = append(statusParts, warnStyle.Render(fmt.Sprintf("%d modified", panel.modified)))
		}
		if panel.staged > 0 {
			statusParts = append(statusParts, successStyle.Render(fmt.Sprintf("%d staged", panel.staged)))
		}
		statusLines = append(statusLines, labelStyle.Render("Status  ")+strings.Join(statusParts, ", "))
	}
	if panel.path != "" {
		pathDisplay := panel.path
		maxPath := width - 12
		if maxPath < 10 {
			maxPath = 10
		}
		if len(pathDisplay) > maxPath {
			pathDisplay = "…" + pathDisplay[len(pathDisplay)-maxPath+1:]
		}
		statusLines = append(statusLines, labelStyle.Render("Path    ")+valueStyle.Render(pathDisplay))
	}

	sessionLines := []string{sectionHeader("Session")}
	if !panel.hasSession {
		sessionLines = append(sessionLines, dimStyle.Render("No active session"))
	} else {
		if panel.windows > 0 || panel.panes > 0 {
			sessionLines = append(sessionLines, fmt.Sprintf("%d windows, %d panes", panel.windows, panel.panes))
		}
		if len(panel.processes) > 0 {
			for _, proc := range panel.processes {
				procDisplay := proc
				if len(procDisplay) > width-6 {
					procDisplay = procDisplay[:width-7] + "…"
				}
				sessionLines = append(sessionLines, dimStyle.Render("○ ")+valueStyle.Render(procDisplay))
			}
		}
	}

	var actionHint string
	if !panel.hasSession {
		actionHint = dimStyle.Render("enter") + " " + subTextStyle.Render("start session")
	} else if panel.isOrphan {
		actionHint = dimStyle.Render("enter") + " " + subTextStyle.Render("actions")
	} else {
		actionHint = dimStyle.Render("enter") + " " + subTextStyle.Render("jump") + "  " +
			dimStyle.Render("tab") + " " + subTextStyle.Render("actions")
	}

	content := name + "\n\n" +
		strings.Join(statusLines, "\n") + "\n\n" +
		strings.Join(sessionLines, "\n") + "\n\n" +
		actionHint

	return sidebarStyle.Render(content)
}

func (m *model) renderCompactTerminal(maxLines int) string {
	if m.paneContent == "" {
		return ""
	}

	boxWidth := m.preview.Width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}

	trafficLights := lipgloss.NewStyle().Foreground(errorColor).Render("●") + " " +
		lipgloss.NewStyle().Foreground(warnColor).Render("●") + " " +
		lipgloss.NewStyle().Foreground(successColor).Render("●")
	
	titleText := m.paneSession
	if len(titleText) > boxWidth-15 {
		titleText = titleText[:boxWidth-18] + "..."
	}
	titleStyle := lipgloss.NewStyle().Foreground(subTextColor)
	
	titleBarContent := trafficLights + "  " + titleStyle.Render(titleText)
	titleBar := lipgloss.NewStyle().
		Background(surfaceBg).
		Width(boxWidth).
		Padding(0, 1).
		Render(titleBarContent)

	termStyle := lipgloss.NewStyle().Foreground(textColor)
	lines := strings.Split(m.paneContent, "\n")
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
		Background(baseBg).
		Width(boxWidth).
		Padding(0, 1)
	termContent := contentStyle.Render(strings.Join(termLines, "\n"))

	return titleBar + "\n" + termContent
}

func (m *model) renderPreviewWithTerminal() string {
	sel, ok := m.list.SelectedItem().(listItem)
	if !ok {
		return dimStyle.Render("Select a worktree")
	}

	var infoContent string
	switch sel.kind {
	case kindCreate:
		if m.globalMode {
			infoContent = m.renderGlobalCreatePreview()
		} else {
			infoContent = m.renderCreatePreview()
		}
	case kindOrphan:
		infoContent = m.renderOrphanPreview(sel.title)
	case kindWorktree:
		wt := sel.data.(workspace.WorktreeState)
		infoContent = m.renderWorktreePreviewNew(wt)
	case kindRecent:
		r := sel.data.(recent.Entry)
		infoContent = m.renderRecentPreview(r)
	case kindGlobal:
		wt := sel.data.(scanner.RepoWorktree)
		infoContent = m.renderGlobalPreview(wt)
	case kindGridView:
		infoContent = m.renderGridViewPreview()
	default:
		infoContent = ""
	}

	terminalPreview := m.renderCompactTerminal(20)
	if terminalPreview != "" {
		return terminalPreview + "\n\n" + infoContent
	}
	return infoContent
}

func (m *model) renderCreatePreview() string {
	title := sectionStyle.Render(iconCreate + " New Worktree")
	steps := []string{
		sectionStyle.Render("1.") + " " + textStyle.Render("Select base branch"),
		dimStyle.Render("2.") + " " + textStyle.Render("Create worktree"),
		dimStyle.Render("3.") + " " + textStyle.Render("Start tmux session"),
	}
	stepsCard := m.renderCard("Workflow", strings.Join(steps, "\n"))
	hint := dimStyle.Render("enter") + " " + subTextStyle.Render("begin")
	return title + "\n\n" + stepsCard + "\n\n" + hint
}

func (m *model) renderGlobalCreatePreview() string {
	title := sectionStyle.Render(iconCreate + " New Worktree")
	steps := []string{
		sectionStyle.Render("1.") + " " + textStyle.Render("Select repository"),
		dimStyle.Render("2.") + " " + textStyle.Render("Select base branch"),
		dimStyle.Render("3.") + " " + textStyle.Render("Create worktree"),
		dimStyle.Render("4.") + " " + textStyle.Render("Start tmux session"),
	}
	stepsCard := m.renderCard("Workflow", strings.Join(steps, "\n"))
	hint := dimStyle.Render("enter") + " " + subTextStyle.Render("begin")
	return title + "\n\n" + stepsCard + "\n\n" + hint
}

func (m *model) renderGridViewPreview() string {
	title := sectionStyle.Render("◫ Grid View")
	
	sessionCount := 0
	if m.globalMode {
		for _, wt := range m.globalWorktrees {
			if m.tmux.HasSession(wt.Worktree.Name) {
				sessionCount++
			}
		}
	} else {
		for _, st := range m.states {
			if st.HasSession {
				sessionCount++
			}
		}
	}
	sessionCount += len(m.orphans)
	
	infoLines := []string{
		m.kvLine("Sessions", textStyle.Render(fmt.Sprintf("%d active", sessionCount))),
	}
	infoCard := m.renderCard("Overview", strings.Join(infoLines, "\n"))
	
	controls := []string{
		dimStyle.Render("arrows") + " " + textStyle.Render("navigate"),
		dimStyle.Render("tab") + " " + textStyle.Render("cycle"),
		dimStyle.Render("enter") + " " + textStyle.Render("jump"),
	}
	controlsCard := m.renderCard("Controls", strings.Join(controls, "\n"))
	
	hint := dimStyle.Render("enter") + " " + subTextStyle.Render("open grid view")
	return title + "\n\n" + infoCard + "\n\n" + controlsCard + "\n\n" + hint
}

func (m *model) renderOrphanPreview(name string) string {
	title := warnStyle.Render(iconOrphan + " Orphaned Session")
	nameDisplay := m.truncatePath(name, m.preview.Width-8)
	infoLines := []string{
		m.kvLine("Session", textStyle.Render(nameDisplay)),
		m.kvLine("Status", warnStyle.Render("No matching worktree")),
	}
	infoCard := m.renderCard(iconSession+" Details", strings.Join(infoLines, "\n"))
	hint := dimStyle.Render("enter") + " " + subTextStyle.Render("jump") + "  " +
		dimStyle.Render("tab") + " " + subTextStyle.Render("actions")
	return title + "\n\n" + infoCard + "\n\n" + hint
}

func (m *model) renderRecentPreview(r recent.Entry) string {
	title := sectionStyle.Render(iconPath + " " + r.RepoName)
	
	maxW := m.preview.Width - 12
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
		m.kvLine("Worktree", textStyle.Render(worktree)),
		m.kvLine("Session", textStyle.Render(r.SessionName)),
		m.kvLine("Path", subTextStyle.Render(pathDisplay)),
	}
	infoCard := m.renderCard(iconBranch+" Details", strings.Join(infoLines, "\n"))
	hint := dimStyle.Render("enter") + " " + subTextStyle.Render("switch to session")
	return title + "\n\n" + infoCard + "\n\n" + hint
}

func (m *model) renderWorktreePreviewNew(wt workspace.WorktreeState) string {
	maxW := m.preview.Width - 12
	if maxW < 20 {
		maxW = 20
	}

	title := currentStyle.Render(iconWorktree + " " + wt.Worktree.Name)
	
	pathDisplay := wt.Worktree.Path
	if len(pathDisplay) > maxW {
		pathDisplay = "..." + pathDisplay[len(pathDisplay)-maxW+3:]
	}

	statusText := "unknown"
	if wt.Status != nil {
		if wt.Status.Clean {
			statusText = successStyle.Render(iconClean + " clean")
		} else {
			parts := []string{}
			if wt.Status.Modified > 0 {
				parts = append(parts, warnStyle.Render(fmt.Sprintf("%d modified", wt.Status.Modified)))
			}
			if wt.Status.Staged > 0 {
				parts = append(parts, sectionStyle.Render(fmt.Sprintf("%d staged", wt.Status.Staged)))
			}
			if wt.Status.Untracked > 0 {
				parts = append(parts, dimStyle.Render(fmt.Sprintf("%d untracked", wt.Status.Untracked)))
			}
			statusText = strings.Join(parts, ", ")
		}
	}

	statusLines := []string{
		m.kvLine("Branch", textStyle.Render(wt.Worktree.Branch)),
		m.kvLine("Status", statusText),
		m.kvLine("Path", subTextStyle.Render(pathDisplay)),
	}

	if wt.Ahead > 0 || wt.Behind > 0 {
		sync := ""
		if wt.Ahead > 0 {
			sync += successStyle.Render(fmt.Sprintf("%d ahead", wt.Ahead))
		}
		if wt.Behind > 0 {
			if sync != "" {
				sync += ", "
			}
			sync += warnStyle.Render(fmt.Sprintf("%d behind", wt.Behind))
		}
		statusLines = append(statusLines, m.kvLine("Sync", sync))
	}

	statusCard := m.renderCard(iconBranch+" Status", strings.Join(statusLines, "\n"))

	var sessionCard string
	if wt.SessionInfo != nil {
		sessionLines := []string{}
		sessionInfo := fmt.Sprintf("%d windows, %d panes", wt.SessionInfo.Windows, wt.SessionInfo.Panes)
		if wt.SessionInfo.IsActive {
			sessionInfo += " " + successStyle.Render("● active")
		}
		sessionLines = append(sessionLines, textStyle.Render(sessionInfo))

		if len(wt.Processes) > 0 && len(wt.Processes) <= 3 {
			sessionLines = append(sessionLines, "")
			for _, p := range wt.Processes {
				status := ClassifyProcess(p)
				var style lipgloss.Style
				switch status {
				case ProcessServer:
					style = successStyle
				case ProcessBuilding:
					style = warnStyle
				default:
					style = subTextStyle
				}
				sessionLines = append(sessionLines, style.Render(status.Icon()+" "+p))
			}
		}
		sessionCard = m.renderCard(iconSession+" Session", strings.Join(sessionLines, "\n"))
	}

	hint := dimStyle.Render("enter") + " " + subTextStyle.Render("jump") + "  " +
		dimStyle.Render("tab") + " " + subTextStyle.Render("actions")

	sections := []string{title, "", statusCard}
	if sessionCard != "" {
		sections = append(sections, "", sessionCard)
	}
	sections = append(sections, "", hint)

	return strings.Join(sections, "\n")
}

func (m *model) kvLine(key, value string) string {
	return dimStyle.Render(fmt.Sprintf("%-8s", key)) + value
}

func (m *model) renderGlobalPreview(wt scanner.RepoWorktree) string {
	maxW := m.preview.Width - 12
	if maxW < 20 {
		maxW = 20
	}

	title := sectionStyle.Render(iconPath + " " + wt.RepoName)
	
	pathDisplay := wt.Worktree.Path
	if len(pathDisplay) > maxW {
		pathDisplay = "..." + pathDisplay[len(pathDisplay)-maxW+3:]
	}

	infoLines := []string{
		m.kvLine("Worktree", textStyle.Render(wt.Worktree.Name)),
		m.kvLine("Branch", textStyle.Render(wt.Worktree.Branch)),
		m.kvLine("Path", subTextStyle.Render(pathDisplay)),
	}

	infoCard := m.renderCard(iconBranch+" Details", strings.Join(infoLines, "\n"))
	hint := dimStyle.Render("enter") + " " + subTextStyle.Render("jump to worktree")

	return strings.Join([]string{title, "", infoCard, "", hint}, "\n")
}

func (m *model) truncatePath(path string, maxLen int) string {
	if maxLen < 10 {
		maxLen = 10
	}
	if len(path) <= maxLen {
		return path
	}
	return "..." + path[len(path)-maxLen+3:]
}

func (m *model) renderCard(title string, content string) string {
	cardWidth := m.preview.Width - 4
	if cardWidth < 20 {
		cardWidth = 20
	}
	
	titleBar := lipgloss.NewStyle().
		Foreground(baseBg).
		Background(accent).
		Bold(true).
		Width(cardWidth).
		Padding(0, 1).
		Render(title)
	
	cardBody := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		BorderTop(false).
		Padding(0, 1).
		Width(cardWidth).
		Render(content)
	
	return titleBar + "\n" + cardBody
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

func renderCommandPalette(m *list.Model, width, height int) string {
	header := titleStyle.Render("  Command Palette")
	divider := separatorStyle.Render("────────────────────────────────")

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
	return modalStyle.Copy().Width(paletteWidth).Render(content)
}

func renderHelp() string {
	helpLine := func(key, desc string) string {
		k := lipgloss.NewStyle().
			Foreground(baseBg).
			Background(teal).
			Bold(true).
			Padding(0, 1).
			Width(10).
			Render(key)
		d := lipgloss.NewStyle().Foreground(textColor).Render("  " + desc)
		return k + d
	}
	
	sectionHeader := func(title string) string {
		return lipgloss.NewStyle().
			Foreground(accent).
			Bold(true).
			MarginTop(1).
			Render("━━ " + title + " ━━")
	}

	t1 := lipgloss.NewStyle().Foreground(lipgloss.Color("#f5c2e7")).Bold(true)
	t2 := lipgloss.NewStyle().Foreground(lipgloss.Color("#cba6f7")).Bold(true)
	t3 := lipgloss.NewStyle().Foreground(lipgloss.Color("#89b4fa")).Bold(true)
	title := lipgloss.NewStyle().Foreground(successColor).Bold(true).Render("▲ ") +
		t1.Render("tree") + t2.Render("mu") + t3.Render("x") +
		dimStyle.Render(" help")

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
	return modalStyle.Render(content)
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

func extractUniqueRepos(worktrees []scanner.RepoWorktree) []repoInfo {
	seen := make(map[string]bool)
	var repos []repoInfo
	for _, wt := range worktrees {
		if !seen[wt.RepoRoot] {
			seen[wt.RepoRoot] = true
			repos = append(repos, repoInfo{name: wt.RepoName, root: wt.RepoRoot})
		}
	}
	return repos
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
	peach        = lipgloss.Color("#fab387")
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
				Border(lipgloss.ThickBorder()).
				BorderForeground(accent)
	modalStyle = lipgloss.NewStyle().
			Padding(1, 2).
			Border(lipgloss.DoubleBorder()).
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
	accentStyle = lipgloss.NewStyle().
			Foreground(accent).
			Bold(true)
	liveBadgeStyle = lipgloss.NewStyle().
			Background(successColor).
			Foreground(baseBg).
			Bold(true).
			Padding(0, 1)
	selectedRowStyle = lipgloss.NewStyle().
			Background(surfaceBg)
)

func sectionTitle(s string) string {
	title := sectionStyle.Render(s)
	return title
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
		accentColor = teal
	case kindGridView:
		accentColor = accent2
	case kindWorktree:
		accentColor = accent
	case kindOrphan:
		accentColor = peach
	case kindRecent:
		accentColor = accent2
	case kindGlobal:
		accentColor = teal
	default:
		accentColor = dimColor
	}

	accentBar := dimStyle.Render("  ")
	if selected {
		accentBar = lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("▌ ")
	}

	var line1, line2 string

	switch i.kind {
	case kindCreate:
		if selected {
			line1 = accentBar + lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("+ New Worktree")
			line2 = accentBar + selectedBranchStyle.Render("  Create worktree and session")
		} else {
			line1 = accentBar + textStyle.Render("+ New Worktree")
			line2 = accentBar + dimStyle.Render("  Create worktree and session")
		}

	case kindGridView:
		icon := "◫"
		if selected {
			line1 = accentBar + lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render(icon + " Grid View")
			line2 = accentBar + lipgloss.NewStyle().Foreground(accentColor).Render("  View all sessions")
		} else {
			line1 = accentBar + textStyle.Render(icon + " Grid View")
			line2 = accentBar + dimStyle.Render("  View all sessions")
		}

	case kindWorktree:
		wt := i.data.(workspace.WorktreeState)
		name := wt.Worktree.Name
		branch := wt.Worktree.Branch

		statusBadge := successStyle.Render(iconClean)
		if wt.Status != nil && !wt.Status.Clean {
			badgeParts := []string{}
			if wt.Status.Modified > 0 {
				badgeParts = append(badgeParts, warnStyle.Render(fmt.Sprintf("%dM", wt.Status.Modified)))
			}
			if wt.Status.Staged > 0 {
				badgeParts = append(badgeParts, sectionStyle.Render(fmt.Sprintf("%dS", wt.Status.Staged)))
			}
			if len(badgeParts) > 0 {
				statusBadge = strings.Join(badgeParts, " ")
			} else if wt.Status.Untracked > 0 {
				statusBadge = dimStyle.Render(fmt.Sprintf("%d?", wt.Status.Untracked))
			}
		}

		indicator := " "
		if i.isCurrent {
			indicator = iconCurrent
		}

		sessionInfo := ""
		if wt.SessionInfo != nil {
			if wt.SessionInfo.IsActive {
				sessionInfo = " " + liveBadgeStyle.Render("LIVE")
			} else {
				sessionInfo = dimStyle.Render(fmt.Sprintf(" %dw %dp", wt.SessionInfo.Windows, wt.SessionInfo.Panes))
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
			line2 = accentBar + "    " + selectedBranchStyle.Render(iconBranch+" "+branchDisplay) + sessionInfo
		} else {
			line1 = accentBar + textStyle.Render(indicator+" "+nameDisplay) + "  " + statusBadge
			line2 = accentBar + "    " + branchStyle.Render(iconBranch+" "+branchDisplay) + sessionInfo
		}

	case kindSeparator:
		line1 = ""
		line2 = ""

	case kindHeader:
		label := i.title
		suffix := ""
		labelStyle := sectionStyle
		if i.title == "ORPHANED SESSIONS" {
			suffix = " (no worktree)"
			labelStyle = warnStyle
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
		line1 = dimStyle.Render(leftBar) + labelStyle.Render(fullLabel) + dimStyle.Render(rightBar)
		line2 = ""

	case kindOrphan:
		name := i.title
		orphanStyle := lipgloss.NewStyle().Foreground(peach).Bold(true)
		if selected {
			line1 = accentBar + orphanStyle.Render(iconSession+" "+name)
			line2 = accentBar + "    " + lipgloss.NewStyle().Foreground(peach).Render("orphaned session")
		} else {
			line1 = accentBar + lipgloss.NewStyle().Foreground(peach).Render(iconSession+" "+name)
			line2 = accentBar + "    " + dimStyle.Render("orphaned session")
		}

	case kindRecent:
		name := i.title
		recentStyle := lipgloss.NewStyle().Foreground(accent2).Bold(true)
		if selected {
			line1 = accentBar + recentStyle.Render(iconJump+" "+name)
			line2 = accentBar + "    " + lipgloss.NewStyle().Foreground(accent2).Render("recent project")
		} else {
			line1 = accentBar + lipgloss.NewStyle().Foreground(accent2).Render(iconJump+" "+name)
			line2 = accentBar + "    " + dimStyle.Render("recent project")
		}

	case kindRepoHeader:
		label := i.title
		fullLabel := " " + iconPath + " " + label + " "
		labelLen := len(label) + 5
		sideLen := (width - labelLen) / 2
		if sideLen < 2 {
			sideLen = 2
		}
		leftBar := strings.Repeat("─", sideLen)
		rightBar := strings.Repeat("─", sideLen)
		line1 = dimStyle.Render(leftBar) + sectionStyle.Render(fullLabel) + dimStyle.Render(rightBar)
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

		globalStyle := lipgloss.NewStyle().Foreground(teal).Bold(true)
		if selected {
			line1 = accentBar + globalStyle.Render(iconWorktree+" "+nameDisplay)
			line2 = accentBar + "    " + lipgloss.NewStyle().Foreground(teal).Render(iconBranch+" "+branchDisplay)
		} else {
			line1 = accentBar + textStyle.Render(iconWorktree+" "+nameDisplay)
			line2 = accentBar + "    " + branchStyle.Render(iconBranch+" "+branchDisplay)
		}
	}

	if selected && i.kind != kindSeparator && i.kind != kindHeader && i.kind != kindRepoHeader {
		rowStyle := lipgloss.NewStyle().Background(surfaceBg).Width(width)
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
