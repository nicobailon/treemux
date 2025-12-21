package tmux

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Tmux struct{}

func (t *Tmux) HasSession(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

func (t *Tmux) NewSession(name, path string) error {
	cmd := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", path)
	return cmd.Run()
}

func (t *Tmux) KillSession(name string) error {
	cmd := exec.Command("tmux", "kill-session", "-t", name)
	return cmd.Run()
}

func (t *Tmux) SwitchClient(name string) error {
	cmd := exec.Command("tmux", "switch-client", "-t", name)
	return cmd.Run()
}

func (t *Tmux) AttachOrCreate(name, path string) error {
	if !t.HasSession(name) {
		if err := t.NewSession(name, path); err != nil {
			return err
		}
	}
	cmd := exec.Command("tmux", "attach", "-t", name)
	return cmd.Run()
}

func (t *Tmux) ListSessions() ([]Session, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	out, err := cmd.Output()
	if err != nil {
		return []Session{}, nil // treat absence of tmux as no sessions
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
	winCmd := exec.Command("tmux", "list-windows", "-t", name)
	winOut, err := winCmd.Output()
	if err != nil {
		return nil, err
	}
	paneCmd := exec.Command("tmux", "list-panes", "-t", name)
	paneOut, err := paneCmd.Output()
	if err != nil {
		return nil, err
	}

	info := &SessionInfo{
		Windows: countNonEmptyLines(string(winOut)),
		Panes:   countNonEmptyLines(string(paneOut)),
	}

	activityCmd := exec.Command("tmux", "display-message", "-t", name, "-p", "#{session_activity}:#{session_attached}")
	activityOut, err := activityCmd.Output()
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
	cmd := exec.Command("tmux", "list-panes", "-t", name, "-F", "#{pane_pid}")
	out, err := cmd.Output()
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
		collectProcessNames(pid, procs)
	}

	var list []string
	for name := range procs {
		list = append(list, name)
	}
	return list, nil
}

func collectProcessNames(pid string, out map[string]struct{}) {
	if pid == "" {
		return
	}
	cmd := exec.Command("ps", "-o", "comm=", "-p", pid)
	if b, err := cmd.Output(); err == nil {
		name := strings.TrimSpace(string(b))
		if name != "" {
			out[name] = struct{}{}
		}
	}
	childCmd := exec.Command("pgrep", "-P", pid)
	childOut, err := childCmd.Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(childOut)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			collectProcessNames(line, out)
		}
	}
}

func (t *Tmux) IsInsideTmux() bool {
	return os.Getenv("TMUX") != ""
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
