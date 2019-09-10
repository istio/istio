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

package restart

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"os"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/hashicorp/go-multierror"
	"golang.org/x/time/rate"

	meshAPI "istio.io/api/mesh/v1alpha1"
	"istio.io/pkg/log"

	"istio.io/istio/pkg/envoy"
	"istio.io/istio/pkg/envoy/bootstrap"
)

// ManagerConfig configures a Manager instance.
type ManagerConfig struct {
	// MaxRestartAttempts for Envoy.
	MaxRestartAttempts uint

	// InitialRestartInterval for Envoy.
	InitialRestartInterval time.Duration

	// Proxy configuration
	Proxy *meshAPI.ProxyConfig

	// Override Envoy bootstrap yaml. If specified, will be provided via the --config-yaml flag.
	ConfigOverride string

	// Pilot service account names.
	PilotSAN []string

	// Node name for Envoy.
	Node string

	// Options specifies additional options used when generating an Envoy bootstrap configuration.
	Options map[string]interface{}

	// NodeIPs specifies the list of IP addresses that Envoy should listen on.
	NodeIPs []string

	// DNSRefreshRate specifies the refresh rate that Envoy should use for DNS.
	DNSRefreshRate string

	// DrainConfigPath is the path to the Envoy bootstrap file to be used when draining Envoy.
	DrainConfigPath string

	// LogLevel for Envoy.
	LogLevel envoy.LogLevel

	// ComponentLogLevels for Envoy.
	ComponentLogLevels envoy.ComponentLogLevels

	// TerminationDrainDuration is the time to allow for the proxy to drain before terminating
	// all remaining proxy processes
	TerminationDrainDuration time.Duration

	// EventHandler for manager events.
	EventHandler EventHandler

	// NewEnvoy is for testing only - should not be set in production. Provides a testing hook
	// for creating Envoy instances.
	NewEnvoy envoy.FactoryFunc

	// StartEnvoy is for testing only - should not be set in production. Provides a testing hook
	// into the logic for starting Envoy.
	StartEnvoy StartEnvoyFunc
}

func (c ManagerConfig) toBootstrapConfig(epoch envoy.Epoch) bootstrap.Config {
	return bootstrap.Config{
		Epoch:          int(epoch),
		Proxy:          c.Proxy,
		ConfigOverride: c.ConfigOverride,
		PilotSAN:       c.PilotSAN,
		Node:           c.Node,
		Options:        c.Options,
		NodeIPs:        c.NodeIPs,
		DNSRefreshRate: c.DNSRefreshRate,
		Env:            os.Environ(),
	}
}

// Manager that attempts to keep Envoy up and running. If an Envoy process exits, it will be restarted based
// on the ManagerConfig parameters.
type Manager interface {
	// ManagerConfig for this Manager
	Config() ManagerConfig

	// Run the manager and start Envoy. If the given context is cancelled, triggers a graceful shutdown of
	// Envoy by switching to a drain configuration for ManagerConfig().TerminationDrainDuration before exiting.
	Run(ctx context.Context) error

	// Restart requests a hot restart of Envoy, causing previous instances to be drained then stopped.
	Restart()
}

// NewManager creates a new manager for Envoy processes.
func NewManager(cfg ManagerConfig) Manager {
	if cfg.NewEnvoy == nil {
		cfg.NewEnvoy = envoy.New
	}
	if cfg.StartEnvoy == nil {
		cfg.StartEnvoy = StartEnvoy
	}
	if cfg.EventHandler == nil {
		cfg.EventHandler = func(_ Event) {}
	}

	return &manager{
		config:        cfg,
		envoyMap:      make(map[envoy.Epoch]envoyWrapper),
		restartCh:     make(chan time.Time, 64),
		exitStatusCh:  make(chan ExitStatus, 1),
		retryStrategy: newRetryStrategy(cfg.MaxRestartAttempts, cfg.InitialRestartInterval),
	}
}

type manager struct {
	config ManagerConfig

	// channel for user restart requests.
	restartCh chan time.Time

	// channel for Envoy epoch exit notifications
	exitStatusCh chan ExitStatus

	retryStrategy *retryStrategy

	lastStartTime time.Time

	shuttingDown bool

	envoyMap map[envoy.Epoch]envoyWrapper
}

func (m *manager) Config() ManagerConfig {
	return m.config
}

func (m *manager) Restart() {
	m.restartCh <- time.Now()
}

func (m *manager) Run(ctx context.Context) (err error) {
	log.Info("Starting Envoy manager")

	// Notify of lifecycle events for the manager.
	m.notifyStarting()
	defer func() {
		m.notifyStopped(err)
	}()

	// Throttle processing up to smoothed 1 qps with bursts up to 10 qps.
	// High QPS is needed to process messages on all channels.
	rateLimiter := rate.NewLimiter(1, 10)
	firstIteration := true

	for {
		err = rateLimiter.Wait(ctx)
		if err != nil {
			return m.terminate()
		}

		// If this is the first time in the run loop, start Envoy for the first time.
		if firstIteration {
			firstIteration = false

			// Start Envoy for the first time.
			if err = m.startEnvoy(); err != nil {
				return err
			}
			continue
		}

		// Get the channel for retrying Envoy startup. Retries will only be scheduled
		// after receiving a state change or after envoy exits.
		retryChan := m.retryStrategy.Chan()

		select {
		case requestTime := <-m.restartCh:
			if err = m.onRestartRequested(requestTime); err != nil {
				return err
			}
		case status := <-m.exitStatusCh:
			if err = m.onEnvoyExited(status); err != nil {
				return err
			}
		case <-retryChan:
			if err = m.onRetryTimer(); err != nil {
				return err
			}
		case <-ctx.Done():
			return m.terminate()
		}
	}
}

func (m *manager) onRestartRequested(requestTime time.Time) error {
	log.Infof("restart of Envoy requested")

	if m.shuttingDown {
		log.Warnf("ignoring request to restart envoy: already shutting down")
		return nil
	}

	if m.lastStartTime.After(requestTime) {
		log.Info("ignoring request to restart envoy: already restarted since the request")
		return nil
	}

	// User requested action, so reset the retry budget.
	m.retryStrategy.Reset()

	// Restart envoy.
	return m.startEnvoy()
}

func (m *manager) onRetryTimer() error {
	return m.startEnvoy()
}

func (m *manager) onEnvoyExited(status ExitStatus) error {
	exitedEnvoy, err := m.removeEnvoy(status.Epoch)
	if err != nil {
		// Should never happen.
		return err
	}

	// Notify handlers that envoy has stopped.
	m.notifyEnvoyStopped(exitedEnvoy, status.Err)

	if status.Err == context.Canceled {
		log.Infof("Epoch %d aborted", status.Epoch)
	} else {
		log.Warnf("Epoch %d terminated with an error: %v", status.Epoch, status.Err)

		// NOTE: due to Envoy hot restart race conditions, an error from the
		// process requires aggressive non-graceful restarts by killing all
		// existing proxy instances
		m.abortAll()
	}

	// Schedule a restart of Envoy.
	if newScheduleCreated, err := m.retryStrategy.Schedule(); err != nil {
		return fmt.Errorf("permanent error: trying to fulfill the desired configuration for Envoy epoch %d: %v",
			status.Epoch, err)
	} else if !newScheduleCreated {
		log.Debugf("Envoy epoch %d: restart already scheduled", status.Epoch)
	}

	// Restart scheduled successfully.
	return nil
}

func (m *manager) removeEnvoy(epoch envoy.Epoch) (envoyWrapper, error) {
	e, ok := m.envoyMap[epoch]
	if !ok {
		return envoyWrapper{}, fmt.Errorf("failed removing envoy for epoch %d", epoch)
	}
	delete(m.envoyMap, epoch)
	return e, nil
}

// abortAll sends abort error to all proxies
func (m *manager) abortAll() {
	for _, e := range m.envoyMap {
		e.abort()
	}
	log.Warnf("Aborted all epochs")
}

func (m *manager) terminate() error {
	m.shuttingDown = true

	// Notify the listener that we're starting graceful shutdown.
	m.notifyShuttingDown()

	// Reset the retry budget to allow us to restart with the envoy configuration.
	m.retryStrategy.Reset()

	// Restart envoy with the drain configuration.
	if err := m.startEnvoy(); err != nil {
		return err
	}

	log.Infof("Graceful termination period is %v, starting...", m.config.TerminationDrainDuration)
	time.Sleep(m.config.TerminationDrainDuration)
	log.Infof("Graceful termination period complete, terminating remaining proxies.")
	m.abortAll()
	return nil
}

func (m *manager) startEnvoy() error {
	// cancel any scheduled restart
	m.retryStrategy.Cancel()

	epoch := m.latestEpoch() + 1

	log.Infof("Epoch %d starting with retry budget %d", epoch, m.retryStrategy.Budget())
	bs, configYaml, err := m.bootstrapAndYaml(epoch)
	if err != nil {
		// This is a hard error. We are unable to generate the bootstrap file from our configuration, which is
		// not likely to be resolved by future attempts. Return an error to terminate the manager.
		return err
	}

	// Create a function to remove the bootstrap config file, if one was generated.
	removeBootstrapFile := func() {
		// Free the bootstrap file, if appropriate.
		if err := bs.Close(); err != nil {
			log.Warnf("Failed to delete config file %s for %d, %v", bs.ConfigPath(), epoch, err)
		}
	}

	options := []envoy.Option{
		epoch,
		envoy.ServiceCluster(m.config.Proxy.ServiceCluster),
		envoy.ServiceNode(m.config.Node),
		envoy.ConfigPath(bs.ConfigPath()),
		envoy.AllowUnknownFields(true),
		envoy.DrainDuration(convertDuration(m.config.Proxy.DrainDuration)),
		envoy.ParentShutdownDuration(convertDuration(m.config.Proxy.ParentShutdownDuration)),
	}

	if configYaml != "" {
		options = append(options, envoy.ConfigYaml(configYaml))
	}

	// Set the concurrency if specified.
	if m.config.Proxy.Concurrency > 0 {
		options = append(options, envoy.Concurrency(uint16(m.config.Proxy.Concurrency)))
	}

	// Set the IP version of the server.
	ipVersion := envoy.IPV4
	if isIPv6Proxy(m.config.NodeIPs) {
		ipVersion = envoy.IPV6
	}
	options = append(options, envoy.LocalAddressIPVersion(ipVersion))

	// Set log levels.
	if m.config.LogLevel != "" {
		options = append(options, m.config.LogLevel)
	}
	if len(m.config.ComponentLogLevels) > 0 {
		options = append(options, m.config.ComponentLogLevels)
	}

	// Create and start the Envoy server.
	e, err := m.config.NewEnvoy(envoy.Config{
		Name:       fmt.Sprintf("epoch %d", epoch),
		BinaryPath: m.config.Proxy.BinaryPath,
		Options:    options,
	})
	if err != nil {
		// This is a hard error. We couldn't create envoy due to a configuration error, which is not likely
		// to be resolved by future attempts. Return an error to terminate the manager.
		removeBootstrapFile()
		return err
	}

	// Notify handlers that envoy is starting.
	m.notifyEnvoyStarting(e)

	ctx, cancel := context.WithCancel(context.Background())

	m.lastStartTime = time.Now()
	m.envoyMap[epoch] = envoyWrapper{
		Instance: e,
		abort:    cancel,
	}

	// When Envoy exits, cancel the context and remove any generated files.
	cleanup := func() {
		cancel()
		removeBootstrapFile()
	}

	// Start Envoy.
	m.config.StartEnvoy(ctx, cleanup, e, m.exitStatusCh)
	return nil
}

// latestEpoch returns the latest epoch, or max uint32 if no epoch is running
func (m *manager) latestEpoch() envoy.Epoch {
	out := int64(-1)
	for e := range m.envoyMap {
		next := int64(e)
		if next > out {
			out = next
		}
	}
	if out < 0 {
		return math.MaxUint32
	}
	return envoy.Epoch(out)
}

func (m *manager) bootstrapAndYaml(epoch envoy.Epoch) (bootstrap.Instance, string, error) {
	// Note: the cert checking still works, the generated file is updated if certs are changed.
	// We just don't save the generated file, but use a custom one instead. Pilot will keep
	// monitoring the certs and restart if the content of the certs changes.
	if m.shuttingDown {
		return bootstrap.New(m.config.DrainConfigPath), "", nil
	}

	var bs bootstrap.Instance
	configYaml := ""
	if m.config.Proxy.CustomConfigFile != "" {
		// there is a custom configuration. Don't write our own config - but keep watching the certs.
		bs = bootstrap.New(m.config.Proxy.CustomConfigFile)
	} else {
		// Generate the config file from the BootstrapConfig.
		var err error
		if bs, err = bootstrap.Generate(m.config.toBootstrapConfig(epoch)); err != nil {
			log.Errora("Failed to generate bootstrap config: ", err)
			return nil, "", err
		}
	}
	// Check if a bootstrap override was supplied.
	if !m.shuttingDown && m.config.ConfigOverride != "" {
		bytes, err := ioutil.ReadFile(m.config.ConfigOverride)
		if err != nil {
			log.Warnf("Failed to read bootstrap override %s, %v", m.config.ConfigOverride, err)
		}
		configYaml = string(bytes)
	}
	return bs, configYaml, nil
}

func (m *manager) notifyStarting() {
	m.config.EventHandler(Event{
		Type: Starting,
	})
}

func (m *manager) notifyShuttingDown() {
	m.config.EventHandler(Event{
		Type: ShuttingDown,
	})
}

func (m *manager) notifyStopped(err error) {
	m.config.EventHandler(Event{
		Type:  Stopped,
		Error: err,
	})
}

func (m *manager) notifyEnvoyStarting(e envoy.Instance) {
	m.config.EventHandler(Event{
		Type:  EnvoyStarting,
		Envoy: e,
	})
}

func (m *manager) notifyEnvoyStopped(e envoy.Instance, err error) {
	m.config.EventHandler(Event{
		Type:  EnvoyStopped,
		Envoy: e,
		Error: err,
	})
}

// envoyWrapper combines an envoy.Manager with an abort function.
type envoyWrapper struct {
	envoy.Instance
	abort func()
}

// ExitStatus is the exit status returned by an Manager. This is public for testing purposes only.
type ExitStatus struct {
	Epoch envoy.Epoch
	Err   error
}

// StartEnvoyFunc is a function for starting Envoy. Visible for testing purposes only.
type StartEnvoyFunc func(ctx context.Context,
	cleanup func(),
	e envoy.Instance,
	exitStatusCh chan<- ExitStatus)

var _ StartEnvoyFunc = StartEnvoy

// StartEnvoy is the production implementation of StartEnvoyFunc. Visible for testing purposes only.
func StartEnvoy(
	ctx context.Context,
	cleanup func(),
	e envoy.Instance,
	exitStatusCh chan<- ExitStatus) {

	// Start the run loop.
	go func() {
		defer cleanup()

		// Notify the agent when this epoch completes.
		var err error
		defer func() {
			// If an error occurred, add a dump of the configuration file.
			err = dumpConfigFileIfAppropriate(err, e.Epoch(), e.ConfigPath())

			exitStatusCh <- ExitStatus{
				Epoch: e.Epoch(),
				Err:   err,
			}
		}()

		// Run the Envoy server.
		e.Start(ctx)

		// Wait for the Envoy server to exit.
		err = e.Wait().Do()
	}()
}

func dumpConfigFileIfAppropriate(in error, epoch envoy.Epoch, configPath string) error {
	switch in {
	case nil, context.Canceled:
		// Don't append the config file if there was no error or if the envoy was aborted.
		return in
	default:
		// For all other errors, append the config file.
		b, err := readConfigFile(epoch, configPath)
		if err != nil {
			return multierror.Append(in, err)
		}
		return multierror.Append(in, errors.New(string(b)))
	}
}

func readConfigFile(epoch envoy.Epoch, configPath string) ([]byte, error) {
	content, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed reading Envoy(epoch %d) configuration file %s: %v",
			epoch,
			configPath,
			err)
	}
	return content, nil
}

// convertDuration converts to golang duration and logs errors
func convertDuration(d *types.Duration) time.Duration {
	if d == nil {
		return 0
	}
	dur, err := types.DurationFromProto(d)
	if err != nil {
		log.Warnf("error converting duration %#v, using 0: %v", d, err)
	}
	return dur
}

// isIPv6Proxy check the addresses slice and returns true for a valid IPv6 address
// for all other cases it returns false
func isIPv6Proxy(ipAddrs []string) bool {
	for i := 0; i < len(ipAddrs); i++ {
		addr := net.ParseIP(ipAddrs[i])
		if addr == nil {
			// Should not happen, invalid IP in proxy's IPAddresses slice should have been caught earlier,
			// skip it to prevent a panic.
			continue
		}
		if addr.To4() != nil {
			return false
		}
	}
	return true
}
