package shell

import (
	"os/exec"
)

type Commander interface {
	Run(name string, args ...string) ([]byte, error)
	RunDir(dir, name string, args ...string) ([]byte, error)
}

type ExecCommander struct{}

func (e *ExecCommander) Run(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

func (e *ExecCommander) RunDir(dir, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}
