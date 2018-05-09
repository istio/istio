//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package envoy

import (
	"fmt"
	"go/build"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

const (
	// DefaultLogLevel the log level used for Envoy if not specified.
	DefaultLogLevel = LogLevelTrace
	maxBaseID       = 1024 * 16
)

// LogLevel represents the log level to use for Envoy.
type LogLevel string

const (
	// LogLevelTrace level
	LogLevelTrace LogLevel = "trace"
	// LogLevelDebug level
	LogLevelDebug LogLevel = "debug"
	// LogLevelInfo level
	LogLevelInfo LogLevel = "info"
	// LogLevelWarning level
	LogLevelWarning LogLevel = "warning"
	// LogLevelError level
	LogLevelError LogLevel = "error"
	// LogLevelCritical level
	LogLevelCritical LogLevel = "critical"
	// LogLevelOff level
	LogLevelOff = "off"
)

// Envoy is a wrapper that simplifies running Envoy.
type Envoy struct {
	// ConfigFile (required) the v2 config file for Envoy.
	ConfigFile string
	// BinPath (optional) the path to the Envoy binary. If not set, uses the debug binary under ISTIO_OUT. If the
	// ISTIO_OUT environment variable is not set, the default location under GOPATH is assumed. If ISTIO_OUT contains
	// multiple debug binaries, the most recent file is used.
	BinPath string
	// LogFilePath (optional) Sets the output log file for Envoy. If not set, Envoy will output to stderr.
	LogFilePath string
	// LogLevel (optional) if provided, sets the log level for Envoy. If not set, DefaultLogLevel will be used.
	LogLevel LogLevel
	cmd      *exec.Cmd
}

// Start starts the Envoy process.
func (e *Envoy) Start() error {
	// Stop if already started.
	if err := e.Stop(); err != nil {
		return err
	}

	if err := e.validateCommandArgs(); err != nil {
		return err
	}

	envoyPath := e.BinPath
	if envoyPath == "" {
		// No binary specified, assume a default location under ISTIO_OUT
		var err error
		envoyPath, err = getDefaultEnvoyBinaryPath()
		if err != nil {
			return err
		}
	}

	// Run the envoy binary
	args := e.getCommandArgs()
	e.cmd = exec.Command(envoyPath, args...)
	e.cmd.Stderr = os.Stderr
	e.cmd.Stdout = os.Stdout
	if err := e.cmd.Start(); err != nil {
		return err
	}
	return nil
}

// Stop kills the Envoy process.
func (e *Envoy) Stop() error {
	if e.cmd == nil || e.cmd.Process != nil {
		// Wasn't previously started - nothing to do.
		return nil
	}

	// Kill the process.
	return e.cmd.Process.Kill()
}

func (e *Envoy) validateCommandArgs() error {
	if e.BinPath != "" {
		// Ensure the binary exists.
		if err := checkFileExists(e.BinPath); err != nil {
			return fmt.Errorf("specified Envoy binary does not exist: %s", e.BinPath)
		}
	}
	if e.ConfigFile == "" {
		return fmt.Errorf("configFile must be specified before running Envoy")
	}
	return nil
}

func (e *Envoy) getCommandArgs() []string {
	// We need to make sure each envoy has a unique base ID in order to run multiple instances on the same
	// machine.
	baseID := rand.Intn(maxBaseID) + 1

	// Prefix Envoy log entries with [ENVOY] to make them distinct from other logs if mixed within the same stream (e.g. stderr)
	logFormat := "[ENVOY] [%Y-%m-%d %T.%e][%t][%l][%n] %v"

	args := []string{
		"--base-id",
		strconv.Itoa(baseID),
		// Always force v2 config.
		"--v2-config-only",
		"--config-path",
		e.ConfigFile,
		"--log-level",
		string(e.getLogLevel()),
		"--log-format",
		logFormat,
	}

	if e.LogFilePath != "" {
		args = append(args, "--log-path", e.LogFilePath)
	}
	return args
}

func (e *Envoy) getLogLevel() LogLevel {
	if e.LogLevel != "" {
		return e.LogLevel
	}
	return DefaultLogLevel
}

func checkFileExists(f string) error {
	if _, err := os.Stat(f); os.IsNotExist(err) {
		return err
	}
	return nil
}

func getDefaultEnvoyBinaryPath() (string, error) {
	istioOut, err := getIstioOut()
	if err != nil {
		return "", err
	}

	// Find all of the debug envoy binaries.
	binDir := filepath.Join(istioOut, "debug")
	binPrefix := filepath.Join(binDir, "envoy-debug-")
	binPaths, err := filepath.Glob(binPrefix + "*")
	if err != nil {
		return "", err
	}

	if len(binPaths) == 0 {
		return "", fmt.Errorf("unable to locate Envoy binary in dir %s", istioOut)
	}

	// Find the most recent debug binary.
	latestBinPath := ""
	latestBinTime := int64(0)
	for _, binPath := range binPaths {
		binFile, err := os.Stat(binPath)
		if err != nil {
			// Should never happen
			return "", err
		}
		binTime := binFile.ModTime().Unix()
		if binTime > latestBinTime {
			latestBinTime = binTime
			latestBinPath = binPath
		}
	}

	return latestBinPath, nil
}

func getIstioOut() (string, error) {
	istioOut := os.Getenv("ISTIO_OUT")
	if istioOut == "" {
		ctx := build.Default
		istioOut = fmt.Sprintf("%s/out/%s_%s", ctx.GOPATH, ctx.GOOS, ctx.GOARCH)
	}
	if err := checkFileExists(istioOut); err != nil {
		return "", fmt.Errorf("unable to resolve ISTIO_OUT. Dir %s does not exist", istioOut)
	}
	return istioOut, nil
}
