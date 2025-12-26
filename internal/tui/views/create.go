package views

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/nicobailon/treemux/internal/scanner"
	"github.com/nicobailon/treemux/internal/tui/theme"
)

type RepoInfo struct {
	Name string
	Root string
}

func ExtractUniqueRepos(worktrees []scanner.RepoWorktree) []RepoInfo {
	seen := make(map[string]bool)
	var repos []RepoInfo
	for _, wt := range worktrees {
		if !seen[wt.RepoRoot] {
			seen[wt.RepoRoot] = true
			repos = append(repos, RepoInfo{Name: wt.RepoName, Root: wt.RepoRoot})
		}
	}
	return repos
}

func FilterStrings(in []string, omit string) []string {
	out := []string{}
	for _, s := range in {
		if s != omit {
			out = append(out, s)
		}
	}
	return out
}

func RenderRepoSelector(menu *list.Model) string {
	return RenderMenu("Select repository", menu)
}

func RenderNameInput(input string) string {
	return RenderPrompt("Create new worktree", "Name:", input)
}

func RenderBranchSelector(menu *list.Model) string {
	return RenderMenu("Base branch", menu)
}

func RenderMenu(title string, m *list.Model) string {
	header := theme.TitleStyle.Render("▲ " + title)
	divider := theme.SeparatorStyle.Render("────────────────────────")
	return theme.ModalStyle.Render(header + "\n" + divider + "\n\n" + m.View())
}

func RenderPrompt(title, label, input string) string {
	header := theme.TitleStyle.Render("▲ " + title)
	divider := theme.SeparatorStyle.Render("────────────────────────")
	labelStyled := theme.SectionStyle.Render(label)
	return theme.ModalStyle.Render(header + "\n" + divider + "\n\n" + labelStyled + "\n" + input)
}
