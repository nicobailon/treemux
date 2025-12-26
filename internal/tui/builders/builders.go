package builders

import (
	"sort"

	"github.com/charmbracelet/bubbles/list"
	"github.com/nicobailon/treemux/internal/recent"
	"github.com/nicobailon/treemux/internal/scanner"
	"github.com/nicobailon/treemux/internal/tui/views"
	"github.com/nicobailon/treemux/internal/workspace"
)

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

type ListItem struct {
	ItemTitle string
	ItemDesc  string
	Kind      ItemKind
	Data      interface{}
	IsCurrent bool
}

func (i ListItem) Title() string       { return i.ItemTitle }
func (i ListItem) Description() string { return i.ItemDesc }
func (i ListItem) FilterValue() string { return i.ItemTitle }

type TmuxChecker interface {
	HasSession(name string) bool
}

type GridBuildInput struct {
	GlobalMode      bool
	GlobalWorktrees []scanner.RepoWorktree
	States          []workspace.WorktreeState
	Orphans         []string
	RecentEntries   []recent.Entry
	Tmux            TmuxChecker
	Width           int
}

type GridBuildOutput struct {
	Panels    []views.GridPanel
	Available []views.GridPanel
	Cols      int
}

func BuildItems(states []workspace.WorktreeState, orphans []string, recentEntries []recent.Entry, currentPath string) []list.Item {
	items := []list.Item{}
	items = append(items, ListItem{
		ItemTitle: "+ Create new worktree ...",
		Kind:      KindCreate,
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
		items = append(items, ListItem{
			ItemTitle: "Grid View",
			Kind:      KindGridView,
		})
	}
	if len(states) > 0 {
		items = append(items, ListItem{ItemTitle: "WORKTREES", Kind: KindHeader})
		for _, st := range states {
			items = append(items, ListItem{
				ItemTitle: st.Worktree.Name,
				Kind:      KindWorktree,
				Data:      st,
				IsCurrent: st.Worktree.Path == currentPath,
			})
		}
	}
	if len(recentEntries) > 0 {
		items = append(items, ListItem{Kind: KindSeparator})
		items = append(items, ListItem{ItemTitle: "RECENT", Kind: KindHeader})
		for _, r := range recentEntries {
			items = append(items, ListItem{
				ItemTitle: r.RepoName + "/" + r.Worktree,
				Kind:      KindRecent,
				Data:      r,
			})
		}
	}
	if len(orphans) > 0 {
		items = append(items, ListItem{Kind: KindSeparator})
		items = append(items, ListItem{ItemTitle: "ORPHANED SESSIONS", Kind: KindHeader})
		for _, o := range orphans {
			items = append(items, ListItem{
				ItemTitle: o,
				Kind:      KindOrphan,
				Data:      o,
			})
		}
	}
	return items
}

func BuildGlobalItems(worktrees []scanner.RepoWorktree, orphans []string, tmux TmuxChecker) []list.Item {
	items := []list.Item{}
	items = append(items, ListItem{
		ItemTitle: "+ New Worktree",
		ItemDesc:  "Create worktree and session",
		Kind:      KindCreate,
	})
	if len(orphans) > 0 || len(worktrees) > 0 {
		items = append(items, ListItem{
			ItemTitle: "Grid View",
			ItemDesc:  "View all sessions",
			Kind:      KindGridView,
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
		items = append(items, ListItem{ItemTitle: "SESSIONS", Kind: KindHeader})
		for _, wt := range withSession {
			items = append(items, ListItem{
				ItemTitle: wt.RepoName + "/" + wt.Worktree.Name,
				ItemDesc:  wt.Worktree.Branch,
				Kind:      KindGlobal,
				Data:      wt,
			})
		}
	}

	if len(withoutSession) > 0 {
		if len(withSession) > 0 {
			items = append(items, ListItem{Kind: KindSeparator})
		}
		items = append(items, ListItem{ItemTitle: "AVAILABLE WORKTREES", Kind: KindHeader})
		for _, wt := range withoutSession {
			items = append(items, ListItem{
				ItemTitle: wt.RepoName + "/" + wt.Worktree.Name,
				ItemDesc:  wt.Worktree.Branch,
				Kind:      KindGlobal,
				Data:      wt,
			})
		}
	}

	if len(orphans) > 0 {
		items = append(items, ListItem{Kind: KindSeparator})
		items = append(items, ListItem{ItemTitle: "ORPHANED SESSIONS (no worktree)", Kind: KindHeader})
		for _, o := range orphans {
			items = append(items, ListItem{
				ItemTitle: o,
				ItemDesc:  "orphaned session",
				Kind:      KindOrphan,
				Data:      o,
			})
		}
	}
	return items
}

func ReorderCurrentFirst(states []workspace.WorktreeState, currentPath string) []workspace.WorktreeState {
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

func BuildGridPanels(in GridBuildInput) GridBuildOutput {
	var panels []views.GridPanel
	var available []views.GridPanel

	if in.GlobalMode {
		for _, wt := range in.GlobalWorktrees {
			sessionName := wt.Worktree.Name
			if in.Tmux.HasSession(sessionName) {
				panels = append(panels, views.GridPanel{
					Name:        wt.RepoName + "/" + wt.Worktree.Name,
					SessionName: sessionName,
					Path:        wt.Worktree.Path,
					Branch:      wt.Worktree.Branch,
					HasSession:  true,
				})
			} else {
				available = append(available, views.GridPanel{
					Name:       wt.RepoName + "/" + wt.Worktree.Name,
					Path:       wt.Worktree.Path,
					Branch:     wt.Worktree.Branch,
					HasSession: false,
				})
			}
		}
	} else {
		for _, st := range in.States {
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
				panels = append(panels, panel)
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
				available = append(available, panel)
			}
		}
	}

	sortedOrphans := make([]string, len(in.Orphans))
	copy(sortedOrphans, in.Orphans)
	sort.Strings(sortedOrphans)
	for _, o := range sortedOrphans {
		panels = append(panels, views.GridPanel{
			Name:        o,
			SessionName: o,
			HasSession:  true,
			IsOrphan:    true,
		})
	}

	if !in.GlobalMode {
		for _, r := range in.RecentEntries {
			sessionName := r.SessionName
			if sessionName == "" {
				sessionName = r.Worktree
			}
			panels = append(panels, views.GridPanel{
				Name:        r.RepoName + "/" + r.Worktree,
				SessionName: sessionName,
				Path:        r.Path,
				HasSession:  in.Tmux.HasSession(sessionName),
				IsRecent:    true,
			})
		}
	}

	gridWidth := in.Width - 4
	if gridWidth < 32 {
		gridWidth = 32
	}
	minPanelWidth := 32
	cols := gridWidth / minPanelWidth
	if cols < 1 {
		cols = 1
	}
	if cols > 4 {
		cols = 4
	}

	return GridBuildOutput{
		Panels:    panels,
		Available: available,
		Cols:      cols,
	}
}
