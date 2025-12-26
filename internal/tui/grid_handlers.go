package tui

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/nicobailon/treemux/internal/tui/views"
)

func handleGridEnter(m *model) (tea.Model, tea.Cmd) {
	if m.nav.State == stateGridView {
		if m.grid.Index == -1 {
			if m.nav.GlobalMode {
				repos := views.ExtractUniqueRepos(m.data.GlobalWorktrees)
				if len(repos) == 0 {
					m.toast = &toast{message: "No repositories found", kind: toastError, expiresAt: time.Now().Add(toastDuration)}
					return *m, toastExpireCmd()
				}
				m.data.AvailableRepos = repos
				items := make([]list.Item, len(repos))
				for i, r := range repos {
					items[i] = listItem{ItemTitle: r.Name, ItemDesc: r.Root, Kind: kindHeader}
				}
				m.menu.SetItems(items)
				m.menu.Select(0)
				m.nav.State = stateSelectRepo
				return *m, nil
			}
			m.nav.State = stateCreateName
			m.input.SetValue("")
			return *m, m.input.Focus()
		}
		if m.grid.Index == -2 {
			m.nav.State = stateMain
			return *m, nil
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
		return *m, nil
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
			return *m, nil
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
			return *m, tea.Quit
		case 1:
			if panel.IsOrphan {
				if m.deps.Svc == nil {
					return *m, nil
				}
				m.pending.Name = panel.SessionName
				m.nav.PrevState = stateGridView
				m.nav.NextBranchState = stateOrphanBranch
				return *m, branchesCmd(m.deps.Svc)
			} else if panel.HasSession && m.deps.Svc != nil {
				return *m, killSessionCmd(m.deps.Svc, panel.SessionName)
			}
		case 2:
			if panel.IsOrphan {
				return *m, killSessionDirectCmd(m.deps.Tmux, panel.SessionName)
			}
		}
		return *m, nil
	}
	return nil, nil
}

func handleGridLeft(m *model) (tea.Model, tea.Cmd) {
	if m.nav.State != stateGridView {
		return nil, nil
	}
	if m.grid.Index == -1 {
		return *m, nil
	}
	if m.grid.Index == -2 {
		m.grid.Index = -1
		m.grid.UpdateScroll(m.width, m.height)
		return *m, nil
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
		return *m, nil
	}
	if m.grid.Index == 0 {
		m.grid.Index = -2
		m.grid.UpdateScroll(m.width, m.height)
		return *m, nil
	}
	if m.grid.Index > 0 {
		m.grid.Index--
	}
	m.grid.UpdateScroll(m.width, m.height)
	return *m, nil
}

func handleGridRight(m *model) (tea.Model, tea.Cmd) {
	if m.nav.State != stateGridView {
		return nil, nil
	}
	if m.grid.Index == -1 {
		m.grid.Index = -2
		m.grid.UpdateScroll(m.width, m.height)
		return *m, nil
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
		return *m, nil
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
	return *m, nil
}

func handleGridUp(m *model) (tea.Model, tea.Cmd) {
	if m.nav.State == stateGridDetail {
		if m.grid.DetailIdx > 0 {
			m.grid.DetailIdx--
		}
		return *m, nil
	}
	if m.nav.State != stateGridView {
		return nil, nil
	}
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
		return *m, nil
	} else if m.grid.Index == -2 {
		m.grid.Index = -1
	} else {
		filteredPanels := m.grid.FilteredPanels()
		if m.grid.Index >= len(filteredPanels) || len(filteredPanels) == 0 {
			m.grid.Index = -2
			m.grid.UpdateScroll(m.width, m.height)
			return *m, nil
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
	return *m, nil
}

func handleGridDown(m *model) (tea.Model, tea.Cmd) {
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
		return *m, nil
	}
	if m.nav.State != stateGridView {
		return nil, nil
	}
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
		return *m, nil
	}
	if m.grid.Index == -1 {
		m.grid.Index = -2
		m.grid.UpdateScroll(m.width, m.height)
		return *m, nil
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
		return *m, nil
	}
	filteredPanels := m.grid.FilteredPanels()
	if m.grid.Index >= len(filteredPanels) || len(filteredPanels) == 0 {
		return *m, nil
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
	return *m, nil
}

func handleGridTab(m *model) (tea.Model, tea.Cmd) {
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
		return *m, nil
	}
	if m.nav.State != stateGridView {
		return nil, nil
	}
	filteredLen := len(m.grid.FilteredPanels())
	if m.grid.Index == -1 {
		m.grid.Index = -2
		m.grid.UpdateScroll(m.width, m.height)
		return *m, nil
	}
	if m.grid.Index == -2 {
		if filteredLen > 0 {
			m.grid.Index = 0
		} else if len(m.grid.FilteredAvailable()) > 0 {
			m.grid.InAvailable = true
			m.grid.AvailIdx = 0
		}
		m.grid.UpdateScroll(m.width, m.height)
		return *m, nil
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
	return *m, nil
}

func handleGridShiftTab(m *model) (tea.Model, tea.Cmd) {
	if m.nav.State != stateGridView {
		return nil, nil
	}
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
		return *m, nil
	}
	if m.grid.Index == -2 {
		m.grid.Index = -1
		m.grid.UpdateScroll(m.width, m.height)
		return *m, nil
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
		return *m, nil
	}
	if m.grid.Index == 0 {
		m.grid.Index = -2
		m.grid.UpdateScroll(m.width, m.height)
		return *m, nil
	}
	if m.grid.Index > 0 {
		m.grid.Index--
	}
	m.grid.UpdateScroll(m.width, m.height)
	return *m, nil
}

func handleGridPageDown(m *model) (tea.Model, tea.Cmd) {
	if m.nav.State != stateGridView {
		return nil, nil
	}
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
	return *m, nil
}

func handleGridPageUp(m *model) (tea.Model, tea.Cmd) {
	if m.nav.State != stateGridView {
		return nil, nil
	}
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
	return *m, nil
}

func handleGridNumberKey(m *model, key string) (tea.Model, tea.Cmd) {
	if m.nav.State != stateGridView {
		return nil, nil
	}
	if m.grid.Filtering {
		m.grid.Filter += key
		m.grid.Index = 0
		return *m, nil
	}
	filteredPanels := m.grid.FilteredPanels()
	idx := int(key[0] - '1')
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
			return *m, nil
		}
		if panel.HasSession {
			m.jumpTarget = &JumpTarget{SessionName: panel.SessionName, Path: panel.Path}
			return *m, tea.Quit
		}
	}
	return *m, nil
}

func handleGridFilterInput(m *model, key string) (tea.Model, tea.Cmd) {
	if m.nav.State != stateGridView || !m.grid.Filtering {
		return nil, nil
	}
	if len(key) == 1 {
		ch := key[0]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' {
			m.grid.Filter += strings.ToLower(key)
			m.grid.Index = 0
			m.grid.InAvailable = false
			m.grid.AvailIdx = 0
			return *m, nil
		}
	}
	return nil, nil
}

func handleGridBackspace(m *model) (tea.Model, tea.Cmd) {
	if m.nav.State != stateGridView || !m.grid.Filtering {
		return nil, nil
	}
	if len(m.grid.Filter) > 0 {
		m.grid.Filter = m.grid.Filter[:len(m.grid.Filter)-1]
		m.grid.Index = 0
	}
	return *m, nil
}

func handleGridSlash(m *model) (tea.Model, tea.Cmd) {
	if m.nav.State != stateGridView || m.grid.Filtering {
		return nil, nil
	}
	m.grid.Filtering = true
	m.grid.Filter = ""
	m.grid.InAvailable = false
	m.grid.Index = 0
	return *m, nil
}

func handleGridEsc(m *model) (tea.Model, tea.Cmd) {
	if m.nav.State == stateGridDetail {
		m.nav.State = stateGridView
		m.grid.DetailPanel = nil
		return *m, nil
	}
	if m.nav.State == stateGridView {
		if m.grid.Filtering {
			m.grid.Filter = ""
			m.grid.Filtering = false
			m.grid.Index = 0
			m.grid.ScrollOffset = 0
			return *m, nil
		}
		m.nav.State = stateMain
		return *m, nil
	}
	return nil, nil
}

func handleGridQ(m *model) (tea.Model, tea.Cmd) {
	if m.nav.State == stateGridView && m.grid.Filtering {
		m.grid.Filter += "q"
		m.grid.Index = 0
		return *m, nil
	}
	return nil, nil
}

func handleGridG(m *model) (tea.Model, tea.Cmd) {
	if m.nav.State == stateGridView && m.grid.Filtering {
		m.grid.Filter += "g"
		m.grid.Index = 0
		return *m, nil
	}
	return nil, nil
}

func handleGridR(m *model) (tea.Model, tea.Cmd) {
	if m.nav.State == stateGridView && m.grid.Filtering {
		m.grid.Filter += "r"
		m.grid.Index = 0
		return *m, nil
	}
	return nil, nil
}
