// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package exec

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"

	shell "github.com/kballard/go-shellquote"
)

// Option enables further configuration of a Cmd.
type Option func(cmd *exec.Cmd)

// WithAdditionalEnvs returns an option that adds additional env vars
// for the given Cmd.
func WithAdditionalEnvs(envs []string) Option {
	return func(c *exec.Cmd) {
		c.Env = append(c.Env, envs...)
	}
}

// WithAdditionalArgs returns an option that adds additional env vars
// for the given Cmd.
func WithAdditionalArgs(args []string) Option {
	return func(c *exec.Cmd) {
		c.Args = append(c.Args, args...)
	}
}

// WithWorkingDir returns an option that sets the working directory for the
// given command.
func WithWorkingDir(dir string) Option {
	return func(c *exec.Cmd) {
		c.Dir = dir
	}
}

// RunWithOutput will run the command with the given options and return the
// output.
// It will wait until the command is finished.
func RunWithOutput(rawCommand string, options ...Option) (string, error) {
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	err := commonRun(rawCommand, io.MultiWriter(os.Stdout, &outBuf), io.MultiWriter(os.Stderr, &errBuf), options...)
	if err != nil {
		return outBuf.String(), fmt.Errorf("%s: %s", err.Error(), errBuf.String())
	}
	return outBuf.String(), nil
}

// Run will run the command with the given options.
// It will wait until the command is finished.
func Run(rawCommand string, options ...Option) error {
	var errBuf bytes.Buffer
	err := commonRun(rawCommand, os.Stdout, io.MultiWriter(os.Stderr, &errBuf), options...)
	if err != nil {
		return fmt.Errorf("%s: %s", err.Error(), errBuf.String())
	}
	return nil
}

// RunMultiple will run the commands with the given options and stop if there is an error.
// It will wait until the command is finished.
func RunMultiple(rawCommands []string, options ...Option) error {
	var err error
	for _, cmd := range rawCommands {
		err = Run(cmd, options...)
		if err != nil {
			return fmt.Errorf("command %q failed with error: %w", cmd, err)
		}
	}
	return nil
}

func commonRun(rawCommand string, stdout, stderr io.Writer, options ...Option) error {
	cmdSplit, err := shell.Split(rawCommand)
	if len(cmdSplit) == 0 || err != nil {
		return fmt.Errorf("error parsing the command %q: %w", rawCommand, err)
	}
	cmd := exec.Command(cmdSplit[0], cmdSplit[1:]...)
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	for _, option := range options {
		option(cmd)
	}

	log.Printf("⚙️ %s", shell.Join(cmd.Args...))
	return cmd.Run()
}
