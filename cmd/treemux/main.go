package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nicobailon/treemux/internal/config"
	"github.com/nicobailon/treemux/internal/deps"
	"github.com/nicobailon/treemux/internal/git"
	"github.com/nicobailon/treemux/internal/recent"
	"github.com/nicobailon/treemux/internal/scanner"
	"github.com/nicobailon/treemux/internal/shell"
	"github.com/nicobailon/treemux/internal/tmux"
	"github.com/nicobailon/treemux/internal/tui"
	"github.com/nicobailon/treemux/internal/workspace"
	"github.com/nicobailon/treemux/pkg/version"
	"github.com/spf13/cobra"
)

var (
	newSession  bool
	listFlag    bool
	versionFlag bool

	listGlobal bool

	createBranch string
	createBase   string
	createRepo   string

	deleteForce bool

	jumpRepo string

	branchesRepo string

	adoptBase string

	recentLimit int
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "treemux",
	Short: "Git worktrees + tmux sessions as one unit",
	RunE:  runRoot,
}

func init() {
	rootCmd.Version = version.Version
	rootCmd.Flags().BoolVar(&newSession, "new-session", false, "internal: launched inside tmux session")
	rootCmd.Flags().MarkHidden("new-session")
	rootCmd.Flags().BoolVarP(&listFlag, "list", "l", false, "List worktrees and orphaned sessions")
	rootCmd.Flags().BoolVarP(&versionFlag, "version", "v", false, "Show version")

	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(cleanCmd)
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(jumpCmd)
	rootCmd.AddCommand(switchCmd)
	rootCmd.AddCommand(killSessionCmd)
	rootCmd.AddCommand(adoptCmd)
	rootCmd.AddCommand(branchesCmd)
	rootCmd.AddCommand(recentCmd)

	listCmd.Flags().BoolVar(&listGlobal, "global", false, "List worktrees across configured search paths")
	createCmd.Flags().StringVar(&createBranch, "branch", "", "Branch to check out or create (defaults to worktree name)")
	createCmd.Flags().StringVar(&createBase, "base", "", "Base branch when creating a new branch")
	createCmd.Flags().StringVar(&createRepo, "repo", "", "Repository path or configured repo name")
	deleteCmd.Flags().BoolVar(&deleteForce, "force", true, "Force delete worktree")
	jumpCmd.Flags().StringVar(&jumpRepo, "repo", "", "Repository path or configured repo name")
	branchesCmd.Flags().StringVar(&branchesRepo, "repo", "", "Repository path or configured repo name")
	adoptCmd.Flags().StringVar(&adoptBase, "base", "", "Base branch when creating a new branch")
	recentCmd.Flags().IntVar(&recentLimit, "limit", 10, "Maximum recent entries to show")
	cleanCmd.Flags().Bool("kill-orphans", false, "Kill orphaned tmux sessions")
}

func ensureDeps() error {
	missing := deps.Check()
	if len(missing) == 0 {
		return nil
	}
	for _, dep := range missing {
		fmt.Fprintf(os.Stderr, "Missing dependency: %s (%s)\n", dep.Name, deps.InstallHint(dep))
	}
	return fmt.Errorf("missing required dependencies")
}

func loadServices() (*config.Config, *git.Git, *tmux.Tmux, *workspace.Service, bool, error) {
	if err := ensureDeps(); err != nil {
		return nil, nil, nil, nil, false, err
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, nil, nil, false, err
	}
	cmd := &shell.ExecCommander{}
	t := &tmux.Tmux{Cmd: cmd}
	g, err := git.New(cmd)
	if err != nil {
		return cfg, nil, t, nil, false, nil
	}
	svc := workspace.NewService(g, t, cfg, cmd)
	return cfg, g, t, svc, true, nil
}

func runRoot(cmd *cobra.Command, args []string) error {
	cfg, g, t, svc, inGitRepo, err := loadServices()
	if err != nil {
		return err
	}

	if versionFlag {
		fmt.Println(version.Version)
		return nil
	}

	if listFlag {
		if !inGitRepo {
			return fmt.Errorf("not in a git repository")
		}
		return listAction(g, svc)
	}

	if inGitRepo && !t.IsInsideTmux() && !newSession {
		if err := bootstrapTmux(g.RepoRoot); err != nil {
			fmt.Fprintf(os.Stderr, "tmux bootstrap failed: %v\n", err)
			fmt.Fprintln(os.Stderr, "falling back to running TUI in current shell (no tmux session created)")
			app := tui.New(svc, cfg, t, inGitRepo)
			target, err := app.Run()
			if err != nil {
				return err
			}
			if target != nil {
				return attachToSession(target.SessionName)
			}
			return nil
		}
		return nil
	}

	app := tui.New(svc, cfg, t, inGitRepo)
	target, err := app.Run()
	if err != nil {
		return err
	}
	if target != nil {
		if target.Create {
			if err := t.NewSession(target.SessionName, target.Path); err != nil {
				return fmt.Errorf("failed to create session: %w", err)
			}
		}
		if t.IsInsideTmux() {
			return t.SwitchClient(target.SessionName)
		}
		return attachToSession(target.SessionName)
	}
	return nil
}

func attachToSession(name string) error {
	cmd := exec.Command("tmux", "attach", "-t", name)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func attachOrSwitchSession(t *tmux.Tmux, sessionName, worktreePath string) error {
	if !t.HasSession(sessionName) {
		if strings.TrimSpace(worktreePath) == "" {
			return fmt.Errorf("session not found: %s", sessionName)
		}
		if err := t.NewSession(sessionName, worktreePath); err != nil {
			return err
		}
	}
	if t.IsInsideTmux() {
		return t.SwitchClient(sessionName)
	}
	return attachToSession(sessionName)
}

func bootstrapTmux(repoRoot string) error {
	sessionName := filepath.Base(repoRoot)
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	if err := exec.Command("tmux", "has-session", "-t", sessionName).Run(); err != nil {
		if out, err := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-c", repoRoot).CombinedOutput(); err != nil {
			return fmt.Errorf("tmux new-session: %v: %s", err, string(out))
		}
		if out, err := exec.Command("tmux", "send-keys", "-t", sessionName, exePath+" --new-session", "Enter").CombinedOutput(); err != nil {
			return fmt.Errorf("tmux send-keys: %v: %s", err, string(out))
		}
		fmt.Fprintf(os.Stdout, "Created session: %s\n", sessionName)
	}
	if out, err := exec.Command("tmux", "attach", "-t", sessionName).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux attach: %v: %s", err, string(out))
	}
	return nil
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List worktrees and orphaned sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, g, t, svc, inGitRepo, err := loadServices()
		if err != nil {
			return err
		}
		if listGlobal {
			return globalListAction(cfg, t)
		}
		if !inGitRepo {
			return fmt.Errorf("not in a git repository")
		}
		return listAction(g, svc)
	},
}

var createCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a worktree and session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		worktreeName := strings.TrimSpace(args[0])
		if worktreeName == "" {
			return fmt.Errorf("worktree name is required")
		}

		cfg, _, t, svc, inGitRepo, err := loadServices()
		if err != nil {
			return err
		}

		targetService, err := resolveServiceForRepo(cfg, t, svc, inGitRepo, createRepo)
		if err != nil {
			return err
		}

		branchName := strings.TrimSpace(createBranch)
		if branchName == "" {
			branchName = worktreeName
		}

		worktreePath, err := targetService.CreateWorktreeFromBranch(worktreeName, branchName, strings.TrimSpace(createBase))
		if err != nil {
			return err
		}

		fmt.Printf("Created worktree: %s\n", worktreeName)
		fmt.Printf("Path: %s\n", worktreePath)
		fmt.Printf("Branch: %s\n", branchName)
		fmt.Printf("Session: %s\n", targetService.SessionName(worktreePath))
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:   "delete <worktree>",
	Short: "Delete a worktree and kill its session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, _, _, svc, inGitRepo, err := loadServices()
		if err != nil {
			return err
		}
		if !inGitRepo || svc == nil {
			return fmt.Errorf("not in a git repository")
		}

		worktree, found, err := findWorktreeByIdentifier(svc, args[0])
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("worktree not found: %s", args[0])
		}
		if worktree.Path == svc.Git.RepoRoot {
			return fmt.Errorf("cannot delete current worktree")
		}

		if err := svc.DeleteWorktree(worktree.Path, deleteForce); err != nil {
			return err
		}
		if err := removeRecentEntry(svc.Git.RepoRoot, worktree.Name); err != nil {
			return err
		}

		fmt.Printf("Deleted worktree: %s\n", worktree.Name)
		return nil
	},
}

var jumpCmd = &cobra.Command{
	Use:   "jump <worktree>",
	Short: "Jump to a worktree session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		identifier := strings.TrimSpace(args[0])
		if identifier == "" {
			return fmt.Errorf("worktree identifier is required")
		}

		cfg, _, t, svc, inGitRepo, err := loadServices()
		if err != nil {
			return err
		}

		target, err := resolveJumpTarget(cfg, t, svc, inGitRepo, identifier, jumpRepo)
		if err != nil {
			return err
		}
		if err := addRecentEntry(target.RepoRoot, target.WorktreeName, target.SessionName, target.Path); err != nil {
			return err
		}
		return attachOrSwitchSession(t, target.SessionName, target.Path)
	},
}

var switchCmd = &cobra.Command{
	Use:   "switch <session>",
	Short: "Switch or attach to an existing tmux session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, _, t, _, _, err := loadServices()
		if err != nil {
			return err
		}

		sessionName := strings.TrimSpace(args[0])
		if sessionName == "" {
			return fmt.Errorf("session name is required")
		}
		if !t.HasSession(sessionName) {
			return fmt.Errorf("session not found: %s", sessionName)
		}

		return attachOrSwitchSession(t, sessionName, "")
	},
}

var killSessionCmd = &cobra.Command{
	Use:   "kill-session <session>",
	Short: "Kill a tmux session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, _, t, _, _, err := loadServices()
		if err != nil {
			return err
		}

		sessionName := strings.TrimSpace(args[0])
		if sessionName == "" {
			return fmt.Errorf("session name is required")
		}
		if !t.HasSession(sessionName) {
			return fmt.Errorf("session not found: %s", sessionName)
		}
		if err := t.KillSession(sessionName); err != nil {
			return err
		}

		fmt.Printf("Killed session: %s\n", sessionName)
		return nil
	},
}

var adoptCmd = &cobra.Command{
	Use:   "adopt <session>",
	Short: "Create a worktree for an orphaned session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, _, _, svc, inGitRepo, err := loadServices()
		if err != nil {
			return err
		}
		if !inGitRepo || svc == nil {
			return fmt.Errorf("not in a git repository")
		}

		sessionName := strings.TrimSpace(args[0])
		if sessionName == "" {
			return fmt.Errorf("session name is required")
		}
		if !svc.Tmux.HasSession(sessionName) {
			return fmt.Errorf("session not found: %s", sessionName)
		}

		_, orphans, err := svc.List()
		if err != nil {
			return err
		}
		if !containsString(orphans, sessionName) {
			return fmt.Errorf("session is not orphaned: %s", sessionName)
		}

		worktreePath, err := svc.AdoptOrphan(sessionName, strings.TrimSpace(adoptBase))
		if err != nil {
			return err
		}

		fmt.Printf("Adopted session: %s\n", sessionName)
		fmt.Printf("Path: %s\n", worktreePath)
		fmt.Printf("Branch: %s\n", sessionName)
		return nil
	},
}

var branchesCmd = &cobra.Command{
	Use:   "branches",
	Short: "List branches for the current or selected repo",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _, t, svc, inGitRepo, err := loadServices()
		if err != nil {
			return err
		}

		targetService, err := resolveServiceForRepo(cfg, t, svc, inGitRepo, branchesRepo)
		if err != nil {
			return err
		}

		branches, err := targetService.Git.Branches()
		if err != nil {
			return err
		}

		defaultBranch := targetService.Git.DefaultBranch()
		branches = reorderDefaultBranch(branches, defaultBranch)

		fmt.Println()
		fmt.Println("Branches")
		for _, branchName := range branches {
			mark := " "
			if branchName == defaultBranch {
				mark = "*"
			}
			fmt.Printf(" %s %s\n", mark, branchName)
		}
		fmt.Println()
		return nil
	},
}

var recentCmd = &cobra.Command{
	Use:   "recent",
	Short: "List recently accessed worktrees",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := recent.Load()
		if err != nil {
			return err
		}

		entries := append([]recent.Entry(nil), store.Entries...)
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].LastAccess.After(entries[j].LastAccess)
		})

		if recentLimit > 0 && len(entries) > recentLimit {
			entries = entries[:recentLimit]
		}

		if len(entries) == 0 {
			fmt.Println()
			fmt.Println("No recent worktrees.")
			fmt.Println()
			return nil
		}

		fmt.Println()
		fmt.Println("Recent worktrees")
		for index, entry := range entries {
			fmt.Printf(" %d. %s/%s %s\n", index+1, entry.RepoName, entry.Worktree, formatRelativeTime(entry.LastAccess))
		}
		fmt.Println()
		return nil
	},
}

func listAction(g *git.Git, svc *workspace.Service) error {
	current := g.RepoRoot
	states, orphans, err := svc.List()
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Worktrees")
	for _, wt := range states {
		mark := " "
		if wt.Worktree.Path == current {
			mark = "●"
		}
		sessionLabel := ""
		if !wt.HasSession {
			sessionLabel = " (no session)"
		}
		fmt.Printf(" %s %-20s %-12s%s\n", mark, wt.Worktree.Name, wt.Worktree.Branch, sessionLabel)
	}
	if len(orphans) > 0 {
		fmt.Println()
		fmt.Println("Orphaned sessions (no worktree)")
		for _, orphan := range orphans {
			fmt.Printf(" - %s\n", orphan)
		}
	}
	fmt.Println()
	return nil
}

type globalWorktreeState struct {
	RepoName    string
	RepoRoot    string
	Worktree    git.Worktree
	SessionName string
	HasSession  bool
}

func globalListAction(cfg *config.Config, t *tmux.Tmux) error {
	allWorktrees := scanner.ScanAll(cfg.SearchPaths)
	if len(allWorktrees) == 0 {
		fmt.Println()
		fmt.Println("No worktrees found in configured search paths.")
		fmt.Println()
		return nil
	}

	sessions, err := t.ListSessions()
	if err != nil {
		return err
	}

	sessionSet := make(map[string]struct{}, len(sessions))
	for _, session := range sessions {
		sessionSet[session.Name] = struct{}{}
	}

	entries := make([]globalWorktreeState, 0, len(allWorktrees))
	worktreeSessionNames := map[string]struct{}{}
	for _, repoWorktree := range allWorktrees {
		repoService := newWorkspaceService(cfg, t, repoWorktree.RepoRoot)
		sessionName := repoService.SessionName(repoWorktree.Worktree.Path)
		_, hasSession := sessionSet[sessionName]
		worktreeSessionNames[sessionName] = struct{}{}
		entries = append(entries, globalWorktreeState{
			RepoName:    repoWorktree.RepoName,
			RepoRoot:    repoWorktree.RepoRoot,
			Worktree:    repoWorktree.Worktree,
			SessionName: sessionName,
			HasSession:  hasSession,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].RepoRoot == entries[j].RepoRoot {
			return entries[i].Worktree.Name < entries[j].Worktree.Name
		}
		return entries[i].RepoRoot < entries[j].RepoRoot
	})

	fmt.Println()
	fmt.Println("Global worktrees")
	currentRepoRoot := ""
	for _, entry := range entries {
		if entry.RepoRoot != currentRepoRoot {
			currentRepoRoot = entry.RepoRoot
			fmt.Printf("\n%s\n", entry.RepoRoot)
		}
		sessionLabel := ""
		if !entry.HasSession {
			sessionLabel = " (no session)"
		}
		fmt.Printf(" - %-20s %-12s%s\n", entry.Worktree.Name, entry.Worktree.Branch, sessionLabel)
	}

	orphans := []string{}
	for sessionName := range sessionSet {
		if _, ok := worktreeSessionNames[sessionName]; !ok {
			orphans = append(orphans, sessionName)
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		fmt.Println()
		fmt.Println("Orphaned sessions (no worktree)")
		for _, orphan := range orphans {
			fmt.Printf(" - %s\n", orphan)
		}
	}

	fmt.Println()
	return nil
}

type commandTarget struct {
	RepoRoot     string
	WorktreeName string
	SessionName  string
	Path         string
}

func resolveServiceForRepo(cfg *config.Config, t *tmux.Tmux, currentService *workspace.Service, inGitRepo bool, repoIdentifier string) (*workspace.Service, error) {
	if strings.TrimSpace(repoIdentifier) == "" {
		if !inGitRepo || currentService == nil {
			return nil, fmt.Errorf("not in a git repository")
		}
		return currentService, nil
	}

	if inGitRepo && currentService != nil {
		if currentService.Git.RepoRoot == repoIdentifier || filepath.Base(currentService.Git.RepoRoot) == repoIdentifier {
			return currentService, nil
		}
	}

	repoRoot, err := resolveRepoRoot(cfg, repoIdentifier)
	if err != nil {
		return nil, err
	}
	return newWorkspaceService(cfg, t, repoRoot), nil
}

func newWorkspaceService(cfg *config.Config, t *tmux.Tmux, repoRoot string) *workspace.Service {
	cmd := &shell.ExecCommander{}
	repoGit := &git.Git{RepoRoot: repoRoot, Cmd: cmd}
	return workspace.NewService(repoGit, t, cfg, cmd)
}

func resolveRepoRoot(cfg *config.Config, repoIdentifier string) (string, error) {
	trimmedIdentifier := strings.TrimSpace(repoIdentifier)
	if trimmedIdentifier == "" {
		return "", fmt.Errorf("repo is required")
	}

	expandedPath := expandPath(trimmedIdentifier)
	if repoRoot, ok := repoRootFromPath(expandedPath); ok {
		return repoRoot, nil
	}

	matches := []string{}
	for _, repoRoot := range scanner.ScanForRepos(cfg.SearchPaths) {
		if repoRoot == trimmedIdentifier || filepath.Base(repoRoot) == trimmedIdentifier {
			matches = append(matches, repoRoot)
		}
	}

	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		sort.Strings(matches)
		return "", fmt.Errorf("repo name is ambiguous: %s (%s)", trimmedIdentifier, strings.Join(matches, ", "))
	}

	return "", fmt.Errorf("repo not found: %s", trimmedIdentifier)
}

func repoRootFromPath(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", false
	}

	out, err := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", false
	}

	return strings.TrimSpace(string(out)), true
}

func expandPath(path string) string {
	expanded := os.ExpandEnv(path)
	if strings.HasPrefix(expanded, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			expanded = filepath.Join(home, expanded[2:])
		}
	}
	absolutePath, err := filepath.Abs(expanded)
	if err == nil {
		return absolutePath
	}
	return expanded
}

func normalizePath(identifier string) string {
	trimmedIdentifier := strings.TrimSpace(identifier)
	if trimmedIdentifier == "" {
		return ""
	}
	return expandPath(trimmedIdentifier)
}

func findWorktreeByIdentifier(svc *workspace.Service, identifier string) (*git.Worktree, bool, error) {
	worktrees, err := svc.Git.WorktreeList()
	if err != nil {
		return nil, false, err
	}

	normalizedIdentifier := normalizePath(identifier)
	for index := range worktrees {
		if worktreeMatchesIdentifier(worktrees[index], identifier, normalizedIdentifier) {
			return &worktrees[index], true, nil
		}
	}

	return nil, false, nil
}

func worktreeMatchesIdentifier(worktree git.Worktree, rawIdentifier, normalizedIdentifier string) bool {
	if worktree.Name == rawIdentifier || worktree.Branch == rawIdentifier || worktree.Path == rawIdentifier {
		return true
	}
	return normalizedIdentifier != "" && worktree.Path == normalizedIdentifier
}

func resolveJumpTarget(cfg *config.Config, t *tmux.Tmux, currentService *workspace.Service, inGitRepo bool, identifier, repoIdentifier string) (*commandTarget, error) {
	if strings.TrimSpace(repoIdentifier) != "" {
		targetService, err := resolveServiceForRepo(cfg, t, currentService, inGitRepo, repoIdentifier)
		if err != nil {
			return nil, err
		}
		worktree, found, err := findWorktreeByIdentifier(targetService, identifier)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("worktree not found: %s", identifier)
		}
		return buildCommandTarget(targetService, worktree), nil
	}

	if inGitRepo && currentService != nil {
		worktree, found, err := findWorktreeByIdentifier(currentService, identifier)
		if err != nil {
			return nil, err
		}
		if found {
			return buildCommandTarget(currentService, worktree), nil
		}
	}

	globalWorktree, err := findGlobalWorktree(cfg, identifier)
	if err != nil {
		return nil, err
	}

	targetService := newWorkspaceService(cfg, t, globalWorktree.RepoRoot)
	return buildCommandTarget(targetService, &globalWorktree.Worktree), nil
}

func buildCommandTarget(svc *workspace.Service, worktree *git.Worktree) *commandTarget {
	sessionName := svc.SessionName(worktree.Path)
	return &commandTarget{
		RepoRoot:     svc.Git.RepoRoot,
		WorktreeName: worktree.Name,
		SessionName:  sessionName,
		Path:         worktree.Path,
	}
}

func findGlobalWorktree(cfg *config.Config, identifier string) (*scanner.RepoWorktree, error) {
	allWorktrees := scanner.ScanAll(cfg.SearchPaths)
	if len(allWorktrees) == 0 {
		return nil, fmt.Errorf("no worktrees found in configured search paths")
	}

	rawIdentifier := strings.TrimSpace(identifier)
	normalizedIdentifier := normalizePath(rawIdentifier)
	for index := range allWorktrees {
		if allWorktrees[index].Worktree.Path == normalizedIdentifier {
			return &allWorktrees[index], nil
		}
	}

	if strings.Contains(rawIdentifier, "/") {
		parts := strings.SplitN(rawIdentifier, "/", 2)
		repoIdentifier := parts[0]
		worktreeIdentifier := parts[1]
		normalizedWorktreeIdentifier := normalizePath(worktreeIdentifier)
		qualifiedMatches := []scanner.RepoWorktree{}
		for _, repoWorktree := range allWorktrees {
			if repoWorktree.RepoName != repoIdentifier && filepath.Base(repoWorktree.RepoRoot) != repoIdentifier && repoWorktree.RepoRoot != repoIdentifier {
				continue
			}
			if worktreeMatchesIdentifier(repoWorktree.Worktree, worktreeIdentifier, normalizedWorktreeIdentifier) {
				qualifiedMatches = append(qualifiedMatches, repoWorktree)
			}
		}
		if len(qualifiedMatches) == 1 {
			return &qualifiedMatches[0], nil
		}
		if len(qualifiedMatches) > 1 {
			return nil, ambiguousWorktreeError(rawIdentifier, qualifiedMatches)
		}
	}

	directMatches := []scanner.RepoWorktree{}
	for _, repoWorktree := range allWorktrees {
		if worktreeMatchesIdentifier(repoWorktree.Worktree, rawIdentifier, normalizedIdentifier) {
			directMatches = append(directMatches, repoWorktree)
		}
	}

	if len(directMatches) == 1 {
		return &directMatches[0], nil
	}
	if len(directMatches) > 1 {
		return nil, ambiguousWorktreeError(rawIdentifier, directMatches)
	}

	return nil, fmt.Errorf("worktree not found: %s", rawIdentifier)
}

func ambiguousWorktreeError(identifier string, matches []scanner.RepoWorktree) error {
	options := make([]string, 0, len(matches))
	for _, match := range matches {
		options = append(options, match.RepoName+"/"+match.Worktree.Name)
	}
	sort.Strings(options)
	return fmt.Errorf("worktree is ambiguous: %s (use one of %s)", identifier, strings.Join(options, ", "))
}

func addRecentEntry(repoRoot, worktreeName, sessionName, worktreePath string) error {
	store, err := recent.Load()
	if err != nil {
		return err
	}
	store.Add(repoRoot, worktreeName, sessionName, worktreePath)
	return store.Save()
}

func removeRecentEntry(repoRoot, worktreeName string) error {
	store, err := recent.Load()
	if err != nil {
		return err
	}
	store.Remove(repoRoot, worktreeName)
	return store.Save()
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func reorderDefaultBranch(branches []string, defaultBranch string) []string {
	reordered := append([]string(nil), branches...)
	sort.Strings(reordered)
	if defaultBranch == "" {
		return reordered
	}

	foundDefault := false
	filteredBranches := make([]string, 0, len(reordered))
	for _, branchName := range reordered {
		if branchName == defaultBranch {
			foundDefault = true
			continue
		}
		filteredBranches = append(filteredBranches, branchName)
	}
	if !foundDefault {
		return reordered
	}

	return append([]string{defaultBranch}, filteredBranches...)
}

func formatRelativeTime(value time.Time) string {
	if value.IsZero() {
		return "unknown"
	}

	elapsed := time.Since(value)
	if elapsed < time.Minute {
		return fmt.Sprintf("%ds ago", int(elapsed.Seconds()))
	}
	if elapsed < time.Hour {
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	}
	if elapsed < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(elapsed.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(elapsed.Hours()/24))
}

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Find and fix orphaned tmux sessions / worktrees without sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, _, t, svc, inGitRepo, err := loadServices()
		if err != nil {
			return err
		}
		if !inGitRepo {
			return fmt.Errorf("not in a git repository")
		}
		states, orphans, err := svc.List()
		if err != nil {
			return err
		}

		kill, _ := cmd.Flags().GetBool("kill-orphans")

		fixed := false
		for _, wt := range states {
			if !wt.HasSession {
				if err := t.NewSession(wt.SessionName, wt.Worktree.Path); err == nil {
					fmt.Printf("Created session for worktree: %s\n", wt.SessionName)
					fixed = true
				}
			}
		}

		if len(orphans) == 0 && !fixed {
			fmt.Println()
			fmt.Println("All clean! No orphans found.")
			fmt.Println()
			return nil
		}

		if len(orphans) > 0 {
			if kill {
				for _, orphan := range orphans {
					if err := t.KillSession(orphan); err == nil {
						fmt.Printf("Killed orphaned session: %s\n", orphan)
					}
				}
			} else {
				fmt.Println("Orphaned sessions detected (use --kill-orphans to remove):")
				for _, orphan := range orphans {
					fmt.Printf(" - %s\n", orphan)
				}
			}
		}

		return nil
	},
}
