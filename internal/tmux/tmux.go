package tmux

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nicobailon/treemux/internal/shell"
)

type Tmux struct {
	Cmd shell.Commander
}

func (t *Tmux) HasSession(name string) bool {
	_, err := t.Cmd.Run("tmux", "has-session", "-t", name)
	return err == nil
}

func (t *Tmux) NewSession(name, path string) error {
	_, err := t.Cmd.Run("tmux", "new-session", "-d", "-s", name, "-c", path)
	return err
}

func (t *Tmux) KillSession(name string) error {
	_, err := t.Cmd.Run("tmux", "kill-session", "-t", name)
	return err
}

func (t *Tmux) SwitchClient(name string) error {
	_, err := t.Cmd.Run("tmux", "switch-client", "-t", name)
	return err
}

func (t *Tmux) AttachOrCreate(name, path string) error {
	if !t.HasSession(name) {
		if err := t.NewSession(name, path); err != nil {
			return err
		}
	}
	_, err := t.Cmd.Run("tmux", "attach", "-t", name)
	return err
}

func (t *Tmux) ListSessions() ([]Session, error) {
	out, err := t.Cmd.Run("tmux", "list-sessions", "-F", "#{session_name}")
	if err != nil {
		return []Session{}, nil
	}
	var sessions []Session
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		info, _ := t.SessionInfo(line)
		sessions = append(sessions, Session{Name: line, Info: info})
	}
	return sessions, nil
}

type Session struct {
	Name string
	Info *SessionInfo
}

type SessionInfo struct {
	Windows      int
	Panes        int
	LastActivity time.Time
	IsActive     bool
}

func (t *Tmux) SessionInfo(name string) (*SessionInfo, error) {
	winOut, err := t.Cmd.Run("tmux", "list-windows", "-t", name)
	if err != nil {
		return nil, err
	}
	paneOut, err := t.Cmd.Run("tmux", "list-panes", "-t", name)
	if err != nil {
		return nil, err
	}

	info := &SessionInfo{
		Windows: countNonEmptyLines(string(winOut)),
		Panes:   countNonEmptyLines(string(paneOut)),
	}

	activityOut, err := t.Cmd.Run("tmux", "display-message", "-t", name, "-p", "#{session_activity}:#{session_attached}")
	if err == nil {
		parts := strings.Split(strings.TrimSpace(string(activityOut)), ":")
		if len(parts) >= 2 {
			if ts, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
				info.LastActivity = time.Unix(ts, 0)
			}
			info.IsActive = parts[1] == "1"
		}
	}

	return info, nil
}

func (t *Tmux) RunningProcesses(name string) ([]string, error) {
	out, err := t.Cmd.Run("tmux", "list-panes", "-t", name, "-F", "#{pane_pid}")
	if err != nil {
		return nil, err
	}
	pids := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			pids = append(pids, line)
		}
	}

	procs := map[string]struct{}{}
	for _, pid := range pids {
		t.collectProcessNames(pid, procs)
	}

	var list []string
	for name := range procs {
		list = append(list, name)
	}
	return list, nil
}

func (t *Tmux) collectProcessNames(pid string, out map[string]struct{}) {
	if pid == "" {
		return
	}
	if b, err := t.Cmd.Run("ps", "-o", "comm=", "-p", pid); err == nil {
		name := strings.TrimSpace(string(b))
		if name != "" {
			out[name] = struct{}{}
		}
	}
	childOut, err := t.Cmd.Run("pgrep", "-P", pid)
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(childOut)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			t.collectProcessNames(line, out)
		}
	}
}

func (t *Tmux) IsInsideTmux() bool {
	return os.Getenv("TMUX") != ""
}

func (t *Tmux) CapturePane(sessionName string, lines int) (string, error) {
	out, err := t.Cmd.Run("tmux", "capture-pane", "-t", sessionName, "-p",
		"-S", fmt.Sprintf("-%d", lines), "-E", "-1")
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func countNonEmptyLines(s string) int {
	cnt := 0
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if strings.TrimSpace(line) != "" {
			cnt++
		}
	}
	return cnt
}
