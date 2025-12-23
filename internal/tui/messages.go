package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type toastType int

const (
	toastSuccess toastType = iota
	toastError
	toastWarning
	toastInfo
)

const toastDuration = 3 * time.Second

type toast struct {
	message   string
	kind      toastType
	expiresAt time.Time
}

func (t *toast) expired() bool {
	return time.Now().After(t.expiresAt)
}

type SuccessMsg struct {
	Message string
}

type ErrorMsg struct {
	Err     error
	Context string
}

func (e ErrorMsg) Error() string {
	if e.Context != "" {
		return fmt.Sprintf("%s: %v", e.Context, e.Err)
	}
	return e.Err.Error()
}

type WarningMsg struct {
	Message string
}

type InfoMsg struct {
	Message string
}

type toastExpiredMsg struct{}

func NewSuccessCmd(message string) tea.Cmd {
	return func() tea.Msg {
		return SuccessMsg{Message: message}
	}
}

func NewErrorCmd(err error, context string) tea.Cmd {
	return func() tea.Msg {
		return ErrorMsg{Err: err, Context: context}
	}
}

func NewWarningCmd(message string) tea.Cmd {
	return func() tea.Msg {
		return WarningMsg{Message: message}
	}
}

func NewInfoCmd(message string) tea.Cmd {
	return func() tea.Msg {
		return InfoMsg{Message: message}
	}
}

func toastExpireCmd() tea.Cmd {
	return tea.Tick(toastDuration, func(time.Time) tea.Msg {
		return toastExpiredMsg{}
	})
}

func (t *toast) render(styles toastStyles) string {
	var style lipgloss.Style
	var icon string

	switch t.kind {
	case toastSuccess:
		style = styles.success
		icon = "  "
	case toastError:
		style = styles.error
		icon = "  "
	case toastWarning:
		style = styles.warning
		icon = "  "
	case toastInfo:
		style = styles.info
		icon = "  "
	}

	return style.Render(icon + t.message)
}

type toastStyles struct {
	success lipgloss.Style
	error   lipgloss.Style
	warning lipgloss.Style
	info    lipgloss.Style
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
