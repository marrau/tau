package shell

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/go-errors/errors"

	"github.com/avinor/tau/pkg/helpers/ui"
)

// Execute a shell command
func Execute(options *Options, command string, args ...string) error {
	if options == nil {
		options = &Options{}
	}

	comma := exec.Command(command, args...)
	comma.Env = os.Environ()

	if options.WorkingDirectory != "" {
		comma.Dir = options.WorkingDirectory
	}

	if len(options.Env) > 0 {
		for k, v := range options.Env {
			variable := fmt.Sprintf("%s=%s", k, v)
			comma.Env = append(comma.Env, variable)
		}
	}

	ui.Debug("environment variables: %#v", comma.Env)
	ui.Debug("command: %s %s", comma.Path, strings.Join(comma.Args, " "))

	comma.Stdout = &executeOutputProcess{
		processors: options.Stdout,
	}
	comma.Stderr = &executeOutputProcess{
		processors: options.Stderr,
	}

	err := comma.Run()
	if err != nil {
		if errorState, ok := err.(*exec.ExitError); ok {
			if errorState.ExitCode() != 0 {
				return errors.Errorf("%s command exited with exit code %v", command, errorState.ExitCode())
			}
		} else {
			return err
		}
	}

	return nil
}

type executeOutputProcess struct {
	processors []OutputProcessor
}

func (eop *executeOutputProcess) Write(p []byte) (n int, err error) {
	for _, out := range eop.processors {
		if !out.Write(string(p)) {
			return
		}
	}
	return len(p), nil
}
