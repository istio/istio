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
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/env"
)

const (
	// DefaultLogLevel the log level used for Envoy if not specified.
	DefaultLogLevel = LogLevelWarning

	// DefaultLogEntryPrefix the default prefix for all log lines from Envoy.
	DefaultLogEntryPrefix = "[ENVOY]"

	envoyFileNamePattern = "^envoy$|^envoy-[a-f0-9]+$|^envoy-debug-[a-f0-9]+$"
)

// LogLevel represents the log level to use for Envoy.
type LogLevel string

const (
	// LogLevelTrace level
	LogLevelTrace LogLevel = "trace"

	// LogLevelWarning level
	LogLevelWarning LogLevel = "warning"
)

// Envoy is a wrapper that simplifies running Envoy.
type Envoy struct {
	// YamlFile (required) the v2 yaml config file for Envoy.
	YamlFile string
	// BinPath (optional) the path to the Envoy binary. If not set, uses the debug binary under ISTIO_OUT. If the
	// ISTIO_OUT environment variable is not set, the default location under GOPATH is assumed. If ISTIO_OUT contains
	// multiple debug binaries, the most recent file is used.
	BinPath string
	// LogFilePath (optional) Sets the output log file for Envoy. If not set, Envoy will output to stderr.
	LogFilePath string
	// LogLevel (optional) if provided, sets the log level for Envoy. If not set, DefaultLogLevel will be used.
	LogLevel LogLevel
	// LogEntryPrefix (optional) if provided, sets the prefix for every log line from this Envoy. Defaults to DefaultLogPrefix.
	LogEntryPrefix string

	cmd    *exec.Cmd
	baseID uint32
}

// Start starts the Envoy process.
func (e *Envoy) Start() (err error) {
	// If there is an error upon exiting this function, stop the server.
	defer func() {
		if err != nil {
			_ = e.Stop()
		}
	}()

	if err = e.validateCommandArgs(); err != nil {
		return err
	}

	envoyPath := e.BinPath
	if envoyPath == "" {
		// No binary specified, assume a default location under ISTIO_OUT
		envoyPath, err = getDefaultEnvoyBinaryPath()
		if err != nil {
			return err
		}
	}

	// We need to make sure each envoy has a unique base ID in order to run multiple instances on the same
	// machine.
	e.baseID = computeBaseID()

	// Run the envoy binary
	args := e.getCommandArgs()
	e.cmd = exec.Command(envoyPath, args...)
	e.cmd.Stderr = os.Stderr
	e.cmd.Stdout = os.Stdout
	return e.cmd.Start()
}

// Stop kills the Envoy process.
// TODO: separate returning of baseID, to make it work with Envoy's hot restart.
func (e *Envoy) Stop() error {
	defer e.freeSharedMemory()

	if e.cmd == nil || e.cmd.Process == nil {
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
	if e.YamlFile == "" {
		return fmt.Errorf("configFile must be specified before running Envoy")
	}
	return nil
}

func (e *Envoy) freeSharedMemory() {
	if e.baseID != 0 {
		// Envoy internally multiplies the base ID from the command line by 10 so that they have spread
		// for domain sockets.
		internalBaseID := int(e.baseID) * 10
		path := "/dev/shm/envoy_shared_memory_" + strconv.Itoa(internalBaseID)
		if err := os.Remove(path); err != nil {
			log.Infof("error deleting Envoy shared memory %s: %v", path, err)
		}
		e.baseID = 0
	}
}

func (e *Envoy) getCommandArgs() []string {
	// Prefix Envoy log entries with [ENVOY] to make them distinct from other logs if mixed within the same stream (e.g. stderr)
	logFormat := e.getLogEntryPrefix() + " [%Y-%m-%d %T.%e][%t][%l][%n] %v"

	args := []string{
		"--base-id",
		strconv.FormatUint(uint64(e.baseID), 10),
		"--config-path",
		e.YamlFile,
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

func (e *Envoy) getLogEntryPrefix() string {
	if e.LogEntryPrefix != "" {
		return e.LogEntryPrefix
	}
	return DefaultLogEntryPrefix
}
func checkFileExists(f string) error {
	if _, err := os.Stat(f); os.IsNotExist(err) {
		return err
	}
	return nil
}

func isEnvoyBinary(f os.FileInfo) bool {
	if f.IsDir() {
		return false
	}
	matches, _ := regexp.MatchString(envoyFileNamePattern, f.Name())
	return matches
}

func findEnvoyBinaries() ([]string, error) {
	binPaths := make([]string, 0)
	err := filepath.Walk(env.IstioOut, func(path string, f os.FileInfo, err error) error {
		if isEnvoyBinary(f) {
			binPaths = append(binPaths, path)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return binPaths, nil
}

func findMostRecentFile(filePaths []string) (string, error) {
	latestFilePath := ""
	latestFileTime := int64(0)
	for _, filePath := range filePaths {
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			// Should never happen
			return "", err
		}
		fileTime := fileInfo.ModTime().Unix()
		if fileTime > latestFileTime {
			latestFileTime = fileTime
			latestFilePath = filePath
		}
	}
	return latestFilePath, nil
}

func getDefaultEnvoyBinaryPath() (string, error) {
	binPaths, err := findEnvoyBinaries()
	if err != nil {
		return "", err
	}

	if len(binPaths) == 0 {
		return "", fmt.Errorf("unable to locate an Envoy binary under dir %s", env.IstioOut)
	}

	latestBinPath, err := findMostRecentFile(binPaths)
	if err != nil {
		return "", err
	}

	return latestBinPath, nil
}

// computeBaseID is a method copied from Envoy server tests.
//
// Computes a numeric ID to incorporate into the names of shared-memory segments and
// domain sockets, to help keep them distinct from other tests that might be running concurrently.
func computeBaseID() uint32 {
	// The PID is needed to isolate namespaces between concurrent processes in CI.
	pid := uint32(os.Getpid())

	// A random number is needed to avoid baseID collisions for multiple Envoys started from the same
	// process.
	randNum := rand.Uint32()

	// Pick a prime number to give more of the 32-bits of entropy to the PID, and the
	// remainder to the random number.
	fourDigitPrime := uint32(7919)
	return pid*fourDigitPrime + randNum%fourDigitPrime
}
