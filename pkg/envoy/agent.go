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

package envoy

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"istio.io/istio/pkg/http"
	"istio.io/istio/pkg/util/sets"
	"istio.io/pkg/log"
)

var errAbort = errors.New("epoch aborted")

const errOutOfMemory = "signal: killed"

var activeConnectionCheckDelay = 1 * time.Second

// NewAgent creates a new proxy agent for the proxy start-up and clean-up functions.
func NewAgent(proxy Proxy, terminationDrainDuration, minDrainDuration time.Duration, localhost string,
	adminPort, statusPort, prometheusPort int, exitOnZeroActiveConnections bool) *Agent {
	knownIstioListeners := sets.New(
		fmt.Sprintf("listener.0.0.0.0_%d.downstream_cx_active", statusPort),
		fmt.Sprintf("listener.0.0.0.0_%d.downstream_cx_active", prometheusPort),
		"listener.admin.downstream_cx_active",
		"listener.admin.main_thread.downstream_cx_active",
	)
	return &Agent{
		proxy:                       proxy,
		statusCh:                    make(chan exitStatus, 1), // context might stop drainage
		drainCh:                     make(chan struct{}),
		abortCh:                     make(chan error, 1),
		terminationDrainDuration:    terminationDrainDuration,
		minDrainDuration:            minDrainDuration,
		exitOnZeroActiveConnections: exitOnZeroActiveConnections,
		adminPort:                   adminPort,
		statusPort:                  statusPort,
		prometheusPort:              prometheusPort,
		localhost:                   localhost,
		knownIstioListeners:         knownIstioListeners,
	}
}

// Proxy defines command interface for a proxy
type Proxy interface {
	// Run command for an epoch, and abort channel
	Run(int, <-chan error) error

	// Drains the current epoch.
	Drain() error

	// Cleanup command for an epoch
	Cleanup(int)

	// UpdateConfig writes a new config file
	UpdateConfig(config []byte) error
}

type Agent struct {
	// proxy commands
	proxy Proxy

	// channel for proxy exit notifications
	statusCh chan exitStatus

	drainCh chan struct{}

	abortCh chan error

	// time to allow for the proxy to drain before terminating all remaining proxy processes
	terminationDrainDuration time.Duration
	minDrainDuration         time.Duration

	adminPort int
	localhost string

	statusPort     int
	prometheusPort int

	knownIstioListeners sets.Set

	exitOnZeroActiveConnections bool
}

type exitStatus struct {
	epoch int
	err   error
}

// Run starts the envoy and waits until it terminates.
func (a *Agent) Run(ctx context.Context) {
	log.Info("Starting proxy agent")
	go a.runWait(0, a.abortCh)

	select {
	case status := <-a.statusCh:
		if status.err != nil {
			if status.err.Error() == errOutOfMemory {
				log.Warnf("Envoy may have been out of memory killed. Check memory usage and limits.")
			}
			log.Errorf("Epoch %d exited with error: %v", status.epoch, status.err)
		} else {
			log.Infof("Epoch %d exited normally", status.epoch)
		}

		log.Infof("No more active epochs, terminating")
	case <-ctx.Done():
		a.terminate()
		status := <-a.statusCh
		if status.err == errAbort {
			log.Infof("Epoch %d aborted normally", status.epoch)
		} else {
			log.Warnf("Epoch %d aborted abnormally", status.epoch)
		}
		log.Info("Agent has successfully terminated")
	}
}

func (a *Agent) terminate() {
	log.Infof("Agent draining Proxy")
	e := a.proxy.Drain()
	if e != nil {
		log.Warnf("Error in invoking drain listeners endpoint %v", e)
	}
	// If exitOnZeroActiveConnections is enabled, always sleep minimumDrainDuration then exit
	// after min(all connections close, terminationGracePeriodSeconds-minimumDrainDuration).
	// exitOnZeroActiveConnections is disabled (default), retain the existing behavior.
	if a.exitOnZeroActiveConnections {
		log.Infof("Agent draining proxy for %v, then waiting for active connections to terminate...", a.minDrainDuration)
		time.Sleep(a.minDrainDuration)
		log.Infof("Checking for active connections...")
		ticker := time.NewTicker(activeConnectionCheckDelay)
		for range ticker.C {
			ac, err := a.activeProxyConnections()
			if err != nil {
				log.Errorf(err.Error())
				a.abortCh <- errAbort
				return
			}
			if ac == -1 {
				log.Info("downstream_cx_active are not available. This either means there are no downstream connection established yet" +
					" or the stats are not enabled. Skipping active connections check...")
				a.abortCh <- errAbort
				return
			}
			if ac == 0 {
				log.Info("There are no more active connections. terminating proxy...")
				a.abortCh <- errAbort
				return
			}
			log.Infof("There are still %d active connections", ac)
		}
	} else {
		log.Infof("Graceful termination period is %v, starting...", a.terminationDrainDuration)
		time.Sleep(a.terminationDrainDuration)
		log.Infof("Graceful termination period complete, terminating remaining proxies.")
		a.abortCh <- errAbort
	}
	log.Warnf("Aborted all epochs")
}

func (a *Agent) activeProxyConnections() (int, error) {
	activeConnectionsURL := fmt.Sprintf("http://%s:%d/stats?usedonly&filter=downstream_cx_active$", a.localhost, a.adminPort)
	stats, err := http.DoHTTPGet(activeConnectionsURL)
	if err != nil {
		return -1, fmt.Errorf("unable to get listener stats from Envoy : %v", err)
	}
	if stats.Len() == 0 {
		return -1, nil
	}
	activeConnections := 0
	for stats.Len() > 0 {
		line, _ := stats.ReadString('\n')
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			log.Warnf("envoy stat line is missing separator. line:%s", line)
			continue
		}
		// downstream_cx_active is accounted under "http." and "listener." for http listeners.
		// Only consider listener stats. Listener stats also will have per worker stats, we can
		// ignore them.
		if !strings.HasPrefix(parts[0], "listener.") || strings.Contains(parts[0], "worker_") {
			continue
		}
		// If the stat is for a known Istio listener skip it.
		if a.knownIstioListeners.Contains(parts[0]) {
			continue
		}
		val, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			log.Warnf("failed parsing Envoy stat %s (error: %s) line: %s", parts[0], err.Error(), line)
			continue
		}
		activeConnections += int(val)
	}
	if activeConnections > 0 {
		log.Debugf("Active connections stats: %s", stats.String())
	}
	return activeConnections, nil
}

// runWait runs the start-up command as a go routine and waits for it to finish
func (a *Agent) runWait(epoch int, abortCh <-chan error) {
	log.Infof("Epoch %d starting", epoch)
	err := a.proxy.Run(epoch, abortCh)
	a.proxy.Cleanup(epoch)
	a.statusCh <- exitStatus{epoch: epoch, err: err}
}
