package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/nicobailon/treemux/internal/config"
	"github.com/nicobailon/treemux/internal/deps"
	"github.com/nicobailon/treemux/internal/git"
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

func loadServices() (*config.Config, *git.Git, *tmux.Tmux, *workspace.Service, error) {
	if err := ensureDeps(); err != nil {
		return nil, nil, nil, nil, err
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	g, err := git.New()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	t := &tmux.Tmux{}
	svc := workspace.NewService(g, t, cfg)
	return cfg, g, t, svc, nil
}

func runRoot(cmd *cobra.Command, args []string) error {
	cfg, g, t, svc, err := loadServices()
	if err != nil {
		return err
	}
	_ = cfg

	if versionFlag {
		fmt.Println(version.Version)
		return nil
	}

	if listFlag {
		return listAction(g, svc)
	}

	if !t.IsInsideTmux() && !newSession {
		if err := bootstrapTmux(g.RepoRoot); err != nil {
			fmt.Fprintf(os.Stderr, "tmux bootstrap failed: %v\n", err)
			fmt.Fprintln(os.Stderr, "falling back to running TUI in current shell (no tmux session created)")
			app := tui.New(svc)
			return app.Run()
		}
		return nil
	}

	app := tui.New(svc)
	return app.Run()
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
		_, g, _, svc, err := loadServices()
		if err != nil {
			return err
		}
		return listAction(g, svc)
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
		for _, o := range orphans {
			fmt.Printf(" - %s\n", o)
		}
	}
	fmt.Println()
	return nil
}

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Find and fix orphaned tmux sessions / worktrees without sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, _, t, svc, err := loadServices()
		if err != nil {
			return err
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
				for _, o := range orphans {
					if err := t.KillSession(o); err == nil {
						fmt.Printf("Killed orphaned session: %s\n", o)
					}
				}
			} else {
				fmt.Println("Orphaned sessions detected (use --kill-orphans to remove):")
				for _, o := range orphans {
					fmt.Printf(" - %s\n", o)
				}
			}
		}

		return nil
	},
}

func init() {
	cleanCmd.Flags().Bool("kill-orphans", false, "Kill orphaned tmux sessions")
}
