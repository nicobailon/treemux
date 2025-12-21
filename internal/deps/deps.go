package deps

import (
	"os/exec"
	"runtime"
)

type Dependency struct {
	Name       string
	Command    string
	Required   bool
	InstallCmd map[string]string
}

type MissingDep struct {
	Dependency
}

var dependencies = []Dependency{
	{
		Name:     "git",
		Command:  "git",
		Required: true,
		InstallCmd: map[string]string{
			"darwin": "brew install git",
			"linux":  "sudo apt install git",
		},
	},
	{
		Name:     "tmux",
		Command:  "tmux",
		Required: true,
		InstallCmd: map[string]string{
			"darwin": "brew install tmux",
			"linux":  "sudo apt install tmux",
		},
	},
}

func Check() []MissingDep {
	missing := []MissingDep{}
	for _, dep := range dependencies {
		if _, err := exec.LookPath(dep.Command); err != nil {
			missing = append(missing, MissingDep{dep})
		}
	}
	return missing
}

func InstallHint(dep MissingDep) string {
	goos := runtime.GOOS
	if cmd, ok := dep.InstallCmd[goos]; ok {
		return cmd
	}
	return "install " + dep.Name + " via your package manager"
}
