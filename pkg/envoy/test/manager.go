// Copyright 2019 Istio Authors
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

package test

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"

	meshAPI "istio.io/api/mesh/v1alpha1"

	"istio.io/istio/pkg/envoy"
	"istio.io/istio/pkg/envoy/restart"
	"istio.io/istio/pkg/test/env"
	testEnvoy "istio.io/istio/pkg/test/envoy"
)

const (
	terminationDrainDuration = 1 * time.Second
	drainDuration            = 5 * time.Second
	parentShutdownDuration   = 2 * time.Second
	serviceCluster           = "testCluster"
	node                     = "node1"
	sdsUdsPath               = "/var/run/sds/uds_path"
	sdsTokenPath             = "/path/to/sds/token"
	dnsRefreshRate           = "300s"
	concurrency              = 4
)

var (
	drainConfigFile        = env.IstioSrc + "/pkg/envoy/test/testdata/drain.json"
	bootstrapTemplateFile  = env.IstioSrc + "/pkg/envoy/test/testdata/bootstrap.json"
	pilotSAN               = []string{"sa1"}
	initialRestartInterval = 1 * time.Millisecond
	maxRestartAttempts     = uint(10)
	nodeIPs                = []string{
		"10.0.0.1",
		"10.0.0.1",
	}
)

type Manager struct {
	Manager      restart.Manager
	cancel       func()
	Config       restart.ManagerConfig
	tempDir      string
	EnvoyFactory *EnvoyFactory
	eventCh      chan restart.Event
	abortFns     map[envoy.Epoch]func()
}

func NewManager(t *testing.T) *Manager {
	t.Helper()

	h := &Manager{
		tempDir:      newTempDir(t),
		EnvoyFactory: NewEnvoyFactory(),
		eventCh:      make(chan restart.Event, 100),
		abortFns:     make(map[envoy.Epoch]func()),
	}

	h.Config = h.DefaultConfig(t)
	return h
}

func (m *Manager) Run() *Manager {
	m.Manager = restart.NewManager(m.Config)

	var ctx context.Context
	ctx, m.cancel = context.WithCancel(context.Background())

	go func() {
		_ = m.Manager.Run(ctx)
	}()
	return m
}

func (m *Manager) Cancel() {
	m.cancel()
}

func (m *Manager) DefaultConfig(t *testing.T) restart.ManagerConfig {
	return restart.ManagerConfig{
		MaxRestartAttempts:     maxRestartAttempts,
		InitialRestartInterval: initialRestartInterval,
		NewEnvoy:               m.EnvoyFactory.New,
		DrainConfigPath:        drainConfigFile,
		Proxy: &meshAPI.ProxyConfig{
			DiscoveryAddress:           "pilot.yadda.com:80",
			ConnectTimeout:             types.DurationProto(5 * time.Second),
			ConfigPath:                 m.tempDir,
			BinaryPath:                 testEnvoy.FindBinaryOrFail(t),
			ServiceCluster:             serviceCluster,
			DrainDuration:              types.DurationProto(drainDuration),
			ParentShutdownDuration:     types.DurationProto(parentShutdownDuration),
			CustomConfigFile:           "",
			Concurrency:                concurrency,
			ProxyBootstrapTemplatePath: bootstrapTemplateFile,
		},
		ConfigOverride: "",
		PilotSAN:       pilotSAN,
		Node:           node,
		Options:        bootstrapOptions(),
		NodeIPs:        nodeIPs,
		DNSRefreshRate: dnsRefreshRate,
		LogLevel:       "info",
		ComponentLogLevels: envoy.ComponentLogLevels{
			envoy.ComponentLogLevel{
				Name:  "c1",
				Level: envoy.LogLevelInfo,
			},
		},
		TerminationDrainDuration: terminationDrainDuration,
		EventHandler: func(e restart.Event) {
			m.eventCh <- e
		},
		StartEnvoy: func(ctx context.Context,
			cleanup func(),
			e envoy.Instance,
			exitStatusCh chan<- restart.ExitStatus) {
			// Intercept the start to save off the abort function.
			m.abortFns[e.Epoch()] = cleanup
			restart.StartEnvoy(ctx, cleanup, e, exitStatusCh)
		},
	}
}

func (m *Manager) AbortEnvoy(epoch envoy.Epoch) {
	m.abortFns[epoch]()
}

func (m *Manager) Restart() *Manager {
	m.Manager.Restart()
	return m
}

func (m *Manager) RestartAndVerifyEnvoy(t *testing.T, epoch envoy.Epoch, drain bool) *Envoy {
	t.Helper()
	return m.Restart().WaitAndVerifyEnvoy(t, epoch, drain)
}

// ExpectNextEvent similar to WaitForEvent but requires that the first event received is of the given type.
func (m *Manager) ExpectNextEvent(t *testing.T, eventType restart.EventType) restart.Event {
	t.Helper()

	timeoutCh := defaultTimeoutCh()
	for {
		select {
		case e := <-m.eventCh:
			if e.Type != eventType {
				t.Fatalf("event %v does not match %v", e.Type, eventType)
			}
			return e
		case <-timeoutCh:
			t.Fatalf("timed out waiting for event %s", eventType)
			return restart.Event{}
		}
	}
}

func (m *Manager) ExpectEventSequence(t *testing.T, types ...restart.EventType) restart.Event {
	t.Helper()

	timeoutCh := defaultTimeoutCh()
	for {
		select {
		case e := <-m.eventCh:
			expected := types[0]
			types = types[1:]

			if e.Type != expected {
				t.Fatalf("event %v does not match %v", e.Type, expected)
			}

			if len(types) == 0 {
				return e
			}
		case <-timeoutCh:
			t.Fatalf("timed out waiting for event %s", types[0])
			return restart.Event{}
		}
	}
}

// ExpectNoEvent waits for a short duration and ensures that no event occurs.
func (m *Manager) ExpectNoEvent(t *testing.T) {
	t.Helper()

	select {
	case e := <-m.eventCh:
		t.Fatalf("unexpected event %v", e.Type)
	case <-time.After(1 * time.Second):
		// Success
		return
	}
}

func (m *Manager) ExpectOneEvent(t *testing.T, eventType restart.EventType) restart.Event {
	t.Helper()
	e := m.ExpectNextEvent(t, eventType)
	m.ExpectNoEvent(t)
	return e
}

// WaitForEvent waits until an event of the given type occurs.
func (m *Manager) WaitForEvent(t *testing.T, eventType restart.EventType) restart.Event {
	t.Helper()

	timeoutCh := defaultTimeoutCh()
	for {
		select {
		case e := <-m.eventCh:
			if e.Type == eventType {
				return e
			}
		case <-timeoutCh:
			t.Fatalf("timed out waiting for event %s", eventType)
			return restart.Event{}
		}
	}
}

func (m *Manager) WaitUntilStopped(t *testing.T) {
	t.Helper()

	m.WaitForEvent(t, restart.Stopped)
}

func (m *Manager) WaitForEnvoy(t *testing.T) *Envoy {
	t.Helper()
	return m.EnvoyFactory.WaitForEnvoy(t)
}

func (m *Manager) WaitForEnvoyWithDuration(t *testing.T, duration time.Duration) *Envoy {
	t.Helper()
	return m.EnvoyFactory.WaitForEnvoyWithDuration(t, duration)
}

func (m *Manager) WaitAndVerifyEnvoy(t *testing.T, epoch envoy.Epoch, drain bool) *Envoy {
	t.Helper()
	e := m.WaitForEnvoy(t)
	m.VerifyEnvoy(t, epoch, e, drain)
	return e
}

func (m *Manager) VerifyEnvoy(t *testing.T, epoch envoy.Epoch, e *Envoy, drain bool) {
	t.Helper()
	// Verify the epoch is configured as expected
	if epoch != e.Epoch() {
		t.Fatalf("expected epoch ID %d to equal %d", e.Epoch(), epoch)
	}

	// Ensure that the correct Envoy binary was used.
	actualBinaryPath := e.Config().BinaryPath
	expectedBinaryPath := m.Config.Proxy.BinaryPath
	if actualBinaryPath != expectedBinaryPath {
		t.Fatalf("expected binary path %s to equal %s", actualBinaryPath, expectedBinaryPath)
	}

	// Get the actual flags that were passed to Envoy.
	actualFlags := flagsForOptions(e.Config().Options)

	restartEpochFlagKey := envoy.Epoch(0).FlagName().String()
	restartEpochValue, ok := actualFlags[restartEpochFlagKey]
	if !ok {
		t.Fatalf("envoy flag %s was not specified", restartEpochValue)
	}
	// Now remove this flag so that we can compare the map directly to the flags from the configuration below.
	delete(actualFlags, restartEpochFlagKey)

	actualEpochID, err := strconv.Atoi(restartEpochValue)
	if err != nil {
		t.Fatalf("failed parsing epoch flag value %s: %v", restartEpochValue, err)
	}
	if epoch != envoy.Epoch(actualEpochID) {
		t.Fatalf("expected epoch flag value %d to match %d", actualEpochID, epoch)
	}

	configPathFlagKey := envoy.ConfigPath("").FlagName().String()
	actualConfigPath, ok := actualFlags[configPathFlagKey]
	if !ok {
		t.Fatalf("envoy flag %s was not specified", configPathFlagKey)
	}
	// Now remove this flag so that we can compare the map directly to the flags from the configuration below.
	delete(actualFlags, configPathFlagKey)

	// Verify that envoy was given the expected config path.
	expectedConfigPath := ""
	if m.Config.Proxy.CustomConfigFile != "" {
		expectedConfigPath = m.Config.Proxy.CustomConfigFile
	} else if drain {
		expectedConfigPath = m.Config.DrainConfigPath
	} else {
		expectedConfigPath = fmt.Sprintf("%s/envoy-rev%d.json", m.Config.Proxy.ConfigPath, actualEpochID)
	}
	if actualConfigPath != expectedConfigPath {
		t.Fatalf("expected config path %s to equal %s", actualConfigPath, expectedConfigPath)
	}

	// Compare the actual flags to the expected flags for the config.
	expectedFlags := flagsForConfig(m.Config)
	if !reflect.DeepEqual(actualFlags, expectedFlags) {
		t.Fatalf("unexpected envoy options: expected\n%v+\nto equal\n%v+", actualFlags, expectedFlags)
	}
}

func (m *Manager) ExpectNoEnvoyCreated(t *testing.T) {
	t.Helper()
	m.EnvoyFactory.ExpectNoEnvoyCreated(t)
}

func (m *Manager) ExpectNoEnvoyCreatedWithDuration(t *testing.T, duration time.Duration) {
	t.Helper()
	m.EnvoyFactory.ExpectNoEnvoyCreatedWithDuration(t, duration)
}

func (m *Manager) Shutdown(t *testing.T) {
	t.Helper()
	// Shutdown the manager, and verify that envoy switched to drain mode.
	m.Cancel()

	m.WaitForEvent(t, restart.ShuttingDown)

	drainCh := make(chan struct{})
	go func() {
		for {
			e := m.WaitForEnvoyWithDuration(t, 5*time.Second)
			if m.IsDraining(e) {
				m.VerifyEnvoy(t, e.Epoch(), e, true)
				close(drainCh)
				return
			}
		}
	}()

	select {
	case <-drainCh:
		break
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for envoy to switch to drain state")
	}

	m.WaitUntilStopped(t)
}

func (m *Manager) IsDraining(e *Envoy) bool {
	return e.ConfigPath() == m.Config.DrainConfigPath
}

func (m *Manager) Cleanup(t *testing.T) {
	t.Helper()
	if m.Manager != nil {
		m.Cancel()
	}

	removeFile(t, m.tempDir)
}

func flagsForOptions(options []envoy.Option) map[string]string {
	out := make(map[string]string)
	for _, o := range options {
		if o.FlagName() != "" {
			out[o.FlagName().String()] = o.FlagValue()
		}
	}
	return out
}

func flagsForConfig(cfg restart.ManagerConfig) map[string]string {
	out := make(map[string]string)

	out[envoy.ServiceNode("").FlagName().String()] = cfg.Node
	out[envoy.ServiceCluster("").FlagName().String()] = cfg.Proxy.GetServiceCluster()
	out[envoy.AllowUnknownFields(true).FlagName().String()] = ""
	out[envoy.ParentShutdownDuration(0).FlagName().String()] = durationSecondsString(cfg.Proxy.ParentShutdownDuration)
	out[envoy.LocalAddressIPVersion(envoy.IPV4).FlagName().String()] = "v4"
	out[envoy.DrainDuration(0).FlagName().String()] = durationSecondsString(cfg.Proxy.DrainDuration)
	out[envoy.Concurrency(0).FlagName().String()] = strconv.Itoa(int(cfg.Proxy.Concurrency))
	out[envoy.LogLevelInfo.FlagName().String()] = cfg.LogLevel.FlagValue()
	out[envoy.ComponentLogLevels{}.FlagName().String()] = cfg.ComponentLogLevels.FlagValue()

	return out
}

func defaultTimeoutCh() <-chan time.Time {
	return time.After(time.Second * 5)
}

func durationSecondsString(p *types.Duration) string {
	d, err := types.DurationFromProto(p)
	if err != nil {
		panic(err)
	}
	return strconv.Itoa(int(d / time.Second))
}

func newTempDir(t *testing.T) string {
	t.Helper()
	dir, err := ioutil.TempDir("", "instance_test")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func removeFile(t *testing.T, f string) {
	t.Helper()
	if err := os.RemoveAll(f); err != nil {
		t.Fatal(err)
	}
}

func bootstrapOptions() map[string]interface{} {
	return map[string]interface{}{
		"sds_uds_path":   sdsUdsPath,
		"sds_token_path": sdsTokenPath,
	}
}
