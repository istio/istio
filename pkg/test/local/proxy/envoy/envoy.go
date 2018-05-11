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
	"bytes"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v2"
	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"

	"istio.io/istio/pkg/test/util"
)

const (
	// DefaultLogLevel the log level used for Envoy if not specified.
	DefaultLogLevel = LogLevelTrace

	healthCheckTimeout  = 10 * time.Second
	healthCheckInterval = 100 * time.Millisecond
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

// HealthCheckState represents a health checking state returned from /server_info
type HealthCheckState string

const (
	// HealthCheckLive indicates Envoy is live and ready to serve requests
	HealthCheckLive HealthCheckState = "live"
	// HealthCheckDraining indicates Envoy is not currently capable of serving requests
	HealthCheckDraining HealthCheckState = "draining"
)

var (
	idGenerator = &baseIDGenerator{
		ids: make(map[uint32]byte),
	}
	nilServerInfo = ServerInfo{}
)

// ServerInfo is the result of a request to /server_info
type ServerInfo struct {
	ProcessName                  string
	CompiledSHABuildType         string
	HealthCheckState             HealthCheckState
	CurrentHotRestartEpochUptime time.Duration
	TotalUptime                  time.Duration
	CurrentHotRestartEpoch       int
}

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

	cmd       *exec.Cmd
	baseID    uint32
	adminPort int
}

// Start starts the Envoy process.
func (e *Envoy) Start() (err error) {
	// If there is an error upon exiting this function, stop the server.
	defer func() {
		if err != nil {
			e.Stop()
		}
	}()

	if err = e.validateCommandArgs(); err != nil {
		return err
	}

	e.adminPort, err = parseAdminPort(e.YamlFile)
	if err != nil {
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
	e.takeBaseID()

	// Run the envoy binary
	args := e.getCommandArgs()
	e.cmd = exec.Command(envoyPath, args...)
	e.cmd.Stderr = os.Stderr
	e.cmd.Stdout = os.Stdout
	if err = e.cmd.Start(); err != nil {
		return err
	}

	return e.waitForHealthCheckLive()
}

// Stop kills the Envoy process.
func (e *Envoy) Stop() error {
	// Make sure we return the base ID.
	defer e.returnBaseID()

	if e.cmd == nil || e.cmd.Process != nil {
		// Wasn't previously started - nothing to do.
		return nil
	}

	// Kill the process.
	return e.cmd.Process.Kill()
}

func parseAdminPort(configFile string) (int, error) {
	yamlBytes, err := ioutil.ReadFile(configFile)
	if err != nil {
		return 0, err
	}
	json, err := yaml.YAMLToJSON(yamlBytes)
	if err != nil {
		return 0, err
	}
	bootstrap := v2.Bootstrap{}
	err = jsonpb.Unmarshal(bytes.NewReader(json), &bootstrap)
	if err != nil {
		return 0, err
	}
	return int(bootstrap.Admin.Address.GetSocketAddress().GetPortValue()), nil
}

// GetAdminPort returns the admin port for the Envoy server.
func (e *Envoy) GetAdminPort() int {
	return e.adminPort
}

// GetServerInfo a structure representing a call to /server_info
func (e *Envoy) GetServerInfo() (ServerInfo, error) {
	requestURL := fmt.Sprintf("http://127.0.0.1:%d/server_info", e.adminPort)
	response, err := http.Get(requestURL)
	if err != nil {
		fmt.Println(err)
		return nilServerInfo, err
	}
	defer response.Body.Close()

	bodyBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nilServerInfo, err
	}

	body := strings.TrimSpace(string(bodyBytes))
	fmt.Println("NM: /server_info returned:")
	fmt.Println(body)
	parts := strings.Split(body, " ")
	if len(parts) != 6 {
		return nilServerInfo, fmt.Errorf("call to /server_info returned invalid response: %s", body)
	}

	currentHotRestartEpochUptime, err := strconv.Atoi(parts[3])
	if err != nil {
		return nilServerInfo, err
	}

	totalUptime, err := strconv.Atoi(parts[4])
	if err != nil {
		return nilServerInfo, err
	}

	currentHotRestartEpoch, err := strconv.Atoi(parts[5])
	if err != nil {
		return nilServerInfo, err
	}

	return ServerInfo{
		ProcessName:                  parts[0],
		CompiledSHABuildType:         parts[1],
		HealthCheckState:             HealthCheckState(parts[2]),
		CurrentHotRestartEpochUptime: time.Second * time.Duration(currentHotRestartEpochUptime),
		TotalUptime:                  time.Second * time.Duration(totalUptime),
		CurrentHotRestartEpoch:       currentHotRestartEpoch,
	}, nil
}

func (e *Envoy) waitForHealthCheckLive() error {
	endTime := time.Now().Add(healthCheckTimeout)
	for {
		var info ServerInfo
		info, err := e.GetServerInfo()
		if err == nil {
			if info.HealthCheckState == HealthCheckLive {
				// It's running, we can return now.
				return nil
			}
		}

		// Stop trying after the timeout
		if time.Now().After(endTime) {
			err = fmt.Errorf("failed to start envoy after %ds. Error: %v", healthCheckTimeout/time.Second, err)
			return err
		}

		// Sleep a short before retry.
		time.Sleep(healthCheckInterval)
	}
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

func (e *Envoy) takeBaseID() {
	e.baseID = idGenerator.takeBaseID()
}

func (e *Envoy) returnBaseID() {
	idGenerator.returnBaseID(e.baseID)
	// Restore the zero value.
	e.baseID = 0
}

func (e *Envoy) getCommandArgs() []string {
	// Prefix Envoy log entries with [ENVOY] to make them distinct from other logs if mixed within the same stream (e.g. stderr)
	logFormat := "[ENVOY] [%Y-%m-%d %T.%e][%t][%l][%n] %v"

	args := []string{
		"--base-id",
		strconv.FormatUint(uint64(e.baseID), 10),
		// Always force v2 config.
		"--v2-config-only",
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

func checkFileExists(f string) error {
	if _, err := os.Stat(f); os.IsNotExist(err) {
		return err
	}
	return nil
}

func getDefaultEnvoyBinaryPath() (string, error) {
	// Find all of the debug envoy binaries.
	binDir := filepath.Join(util.IstioOut, "debug")
	binPrefix := filepath.Join(binDir, "envoy-debug-")
	binPaths, err := filepath.Glob(binPrefix + "*")
	if err != nil {
		return "", err
	}

	if len(binPaths) == 0 {
		return "", fmt.Errorf("unable to locate Envoy binary in dir %s", util.IstioOut)
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

// A little utility that helps to ensure that we don't re-use
type baseIDGenerator struct {
	m   sync.Mutex
	ids map[uint32]byte
}

func (g *baseIDGenerator) takeBaseID() uint32 {
	g.m.Lock()
	defer g.m.Unlock()

	// Retry until we find a baseID that's not currently in use.
	for {
		baseID := rand.Uint32()
		// Don't allow 0, since we treat that as not-set.
		if baseID > 0 {
			_, ok := g.ids[baseID]
			if !ok {
				g.ids[baseID] = 1
				return baseID
			}
		}
	}
}

func (g *baseIDGenerator) returnBaseID(baseID uint32) {
	g.m.Lock()
	defer g.m.Unlock()
	delete(g.ids, baseID)
}
