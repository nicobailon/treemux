package recent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const maxRecent = 10

type Entry struct {
	RepoRoot    string    `json:"repo_root"`
	RepoName    string    `json:"repo_name"`
	Worktree    string    `json:"worktree"`
	SessionName string    `json:"session_name"`
	Path        string    `json:"path"`
	LastAccess  time.Time `json:"last_access"`
}

type Store struct {
	Entries []Entry `json:"entries"`
	path    string
}

func configDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "treemux")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "treemux")
}

func Load() (*Store, error) {
	dir := configDir()
	path := filepath.Join(dir, "recent.json")

	s := &Store{path: path, Entries: []Entry{}}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, s); err != nil {
		return s, nil
	}
	s.path = path
	return s, nil
}

func (s *Store) Save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0644)
}

func (s *Store) Add(repoRoot, worktree, sessionName, wtPath string) {
	repoName := filepath.Base(repoRoot)

	for i, e := range s.Entries {
		if e.RepoRoot == repoRoot && e.Worktree == worktree {
			s.Entries[i].LastAccess = time.Now()
			s.Entries[i].SessionName = sessionName
			s.Entries[i].Path = wtPath
			return
		}
	}

	s.Entries = append(s.Entries, Entry{
		RepoRoot:    repoRoot,
		RepoName:    repoName,
		Worktree:    worktree,
		SessionName: sessionName,
		Path:        wtPath,
		LastAccess:  time.Now(),
	})

	s.prune()
}

func (s *Store) prune() {
	sort.Slice(s.Entries, func(i, j int) bool {
		return s.Entries[i].LastAccess.After(s.Entries[j].LastAccess)
	})

	if len(s.Entries) > maxRecent*3 {
		s.Entries = s.Entries[:maxRecent*3]
	}
}

func (s *Store) GetOtherProjects(currentRepoRoot string, limit int) []Entry {
	var others []Entry
	for _, e := range s.Entries {
		if e.RepoRoot != currentRepoRoot {
			others = append(others, e)
		}
	}

	sort.Slice(others, func(i, j int) bool {
		return others[i].LastAccess.After(others[j].LastAccess)
	})

	if len(others) > limit {
		others = others[:limit]
	}

	return others
}

func (s *Store) Remove(repoRoot, worktree string) {
	var filtered []Entry
	for _, e := range s.Entries {
		if !(e.RepoRoot == repoRoot && e.Worktree == worktree) {
			filtered = append(filtered, e)
		}
	}
	s.Entries = filtered
}
