// Copyright 2017 Istio Authors
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

package proxy

import (
	"context"
	"errors"
	"reflect"
	"time"

	"golang.org/x/time/rate"

	"istio.io/istio/pkg/log"
)

// Agent manages the restarts and the life cycle of a proxy binary.  Agent
// keeps track of all running proxy epochs and their configurations.  Hot
// restarts are performed by launching a new proxy process with a strictly
// incremented restart epoch. It is up to the proxy to ensure that older epochs
// gracefully shutdown and carry over all the necessary state to the latest
// epoch.  The agent does not terminate older epochs. The initial epoch is 0.
//
// The restart protocol matches Envoy semantics for restart epochs: to
// successfully launch a new Envoy process that will replace the running Envoy
// processes, the restart epoch of the new process must be exactly 1 greater
// than the highest restart epoch of the currently running Envoy processes.
// See https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/hot_restart.html
// for more information about the Envoy hot restart protocol.
//
// Agent requires two functions "run" and "cleanup". Run function is a call to
// start the proxy and must block until the proxy exits. Cleanup function is
// executed immediately after the proxy exits and must be non-blocking since it
// is executed synchronously in the main agent control loop. Both functions
// take the proxy epoch as an argument. A typical scenario would involve epoch
// 0 followed by a failed epoch 1 start. The agent then attempts to start epoch
// 1 again.
//
// Whenever the run function returns an error, the agent assumes that the proxy
// failed to start and attempts to restart the proxy several times with an
// exponential back-off. The subsequent restart attempts may reuse the epoch
// from the failed attempt. Retry budgets are allocated whenever the desired
// configuration changes.
//
// Agent executes a single control loop that receives notifications about
// scheduled configuration updates, exits from older proxy epochs, and retry
// attempt timers. The call to schedule a configuration update will block until
// the control loop is ready to accept and process the configuration update.
type Agent interface {
	// ConfigCh returns the config channel used to send configuration updates.
	// Agent compares the current active configuration to the desired state and
	// initiates a restart if necessary. If the restart fails, the agent attempts
	// to retry with an exponential back-off.
	ConfigCh() chan<- interface{}

	// Run starts the agent control loop and awaits for a signal on the input
	// channel to exit the loop.
	Run(ctx context.Context)
}

var (
	errAbort = errors.New("epoch aborted")

	// DefaultRetry configuration for proxies
	DefaultRetry = Retry{
		MaxRetries:      10,
		InitialInterval: 200 * time.Millisecond,
	}
)

const (
	// maxAborts is the maximum number of cascading abort messages to buffer.
	// This should be the upper bound on the number of proxies available at any point in time.
	maxAborts = 10
)

// NewAgent creates a new proxy agent for the proxy start-up and clean-up functions.
func NewAgent(proxy Proxy, retry Retry, terminationDrainDuration time.Duration) Agent {
	return &agent{
		proxy:                    proxy,
		retry:                    retry,
		epochs:                   make(map[int]interface{}),
		configCh:                 make(chan interface{}),
		statusCh:                 make(chan exitStatus),
		abortCh:                  make(map[int]chan error),
		terminationDrainDuration: terminationDrainDuration,
	}
}

// Retry configuration for the proxy
type Retry struct {
	// restart is the timestamp of the next scheduled restart attempt
	restart *time.Time

	// number of times to attempts left to retry applying the latest desired configuration
	budget int

	// MaxRetries is the maximum number of retries
	MaxRetries int

	// InitialInterval is the delay between the first restart, from then on it is
	// multiplied by a factor of 2 for each subsequent retry
	InitialInterval time.Duration
}

// Proxy defines command interface for a proxy
type Proxy interface {
	// Run command for a config, epoch, and abort channel
	Run(interface{}, int, <-chan error) error

	// Cleanup command for an epoch
	Cleanup(int)

	// Panic command is invoked with the desired config when all retries to
	// start the proxy fail just before the agent terminating
	Panic(interface{})
}

// DrainConfig is used to signal to the Proxy that it should start draining connections
type DrainConfig struct{}

type agent struct {
	// proxy commands
	proxy Proxy

	// retry configuration
	retry Retry

	// desired configuration state
	desiredConfig interface{}

	// active epochs and their configurations
	epochs map[int]interface{}

	// current configuration is the highest epoch configuration
	currentConfig interface{}

	// channel for posting desired configurations
	configCh chan interface{}

	// channel for proxy exit notifications
	statusCh chan exitStatus

	// channel for aborting running instances
	abortCh map[int]chan error

	// time to allow for the proxy to drain before terminating all remaining proxy processes
	terminationDrainDuration time.Duration
}

type exitStatus struct {
	epoch int
	err   error
}

func (a *agent) ConfigCh() chan<- interface{} {
	return a.configCh
}

func (a *agent) Run(ctx context.Context) {
	log.Info("Starting proxy agent")

	// Throttle processing up to smoothed 1 qps with bursts up to 10 qps.
	// High QPS is needed to process messages on all channels.
	rateLimiter := rate.NewLimiter(1, 10)

	var reconcileTimer *time.Timer
	for {
		err := rateLimiter.Wait(ctx)
		if err != nil {
			a.terminate()
			return
		}

		// maximum duration or duration till next restart
		var delay time.Duration = 1<<63 - 1
		if a.retry.restart != nil {
			delay = time.Until(*a.retry.restart)
		}
		if reconcileTimer != nil {
			reconcileTimer.Stop()
		}
		reconcileTimer = time.NewTimer(delay)

		select {
		case config := <-a.configCh:
			if !reflect.DeepEqual(a.desiredConfig, config) {
				log.Infof("Received new config, resetting budget")
				a.desiredConfig = config

				// reset retry budget if and only if the desired config changes
				a.retry.budget = a.retry.MaxRetries
				a.reconcile()
			}

		case status := <-a.statusCh:
			// delete epoch record and update current config
			// avoid self-aborting on non-abort error
			delete(a.epochs, status.epoch)
			delete(a.abortCh, status.epoch)
			a.currentConfig = a.epochs[a.latestEpoch()]

			if status.err == errAbort {
				log.Infof("Epoch %d aborted", status.epoch)
			} else if status.err != nil {
				log.Warnf("Epoch %d terminated with an error: %v", status.epoch, status.err)

				// NOTE: due to Envoy hot restart race conditions, an error from the
				// process requires aggressive non-graceful restarts by killing all
				// existing proxy instances
				a.abortAll()
			} else {
				log.Infof("Epoch %d exited normally", status.epoch)
			}

			// cleanup for the epoch
			a.proxy.Cleanup(status.epoch)

			// schedule a retry for an error.
			// the current config might be out of date from here since its proxy might have been aborted.
			// the current config will change on abort, hence retrying prior to abort will not progress.
			// that means that aborted envoy might need to re-schedule a retry if it was not already scheduled.
			if status.err != nil {
				// skip retrying twice by checking retry restart delay
				if a.retry.restart == nil {
					if a.retry.budget > 0 {
						delayDuration := a.retry.InitialInterval * (1 << uint(a.retry.MaxRetries-a.retry.budget))
						restart := time.Now().Add(delayDuration)
						a.retry.restart = &restart
						a.retry.budget = a.retry.budget - 1
						log.Infof("Epoch %d: set retry delay to %v, budget to %d", status.epoch, delayDuration, a.retry.budget)
					} else {
						log.Error("Permanent error: budget exhausted trying to fulfill the desired configuration")
						a.proxy.Panic(status.epoch)
						return
					}
				} else {
					log.Debugf("Epoch %d: restart already scheduled", status.epoch)
				}
			}

		case <-reconcileTimer.C:
			a.reconcile()

		case <-ctx.Done():
			a.terminate()
			log.Info("Agent has successfully terminated")
			return
		}
	}
}

func (a *agent) terminate() {
	log.Infof("Agent draining Proxy")
	a.desiredConfig = DrainConfig{}
	a.reconcile()
	log.Infof("Graceful termination period is %v, starting...", a.terminationDrainDuration)
	time.Sleep(a.terminationDrainDuration)
	log.Infof("Graceful termination period complete, terminating remaining proxies.")
	a.abortAll()
}

func (a *agent) reconcile() {
	// cancel any scheduled restart
	a.retry.restart = nil

	log.Infof("Reconciling retry (budget %d)", a.retry.budget)

	// check that the config is current
	if reflect.DeepEqual(a.desiredConfig, a.currentConfig) {
		log.Infof("Desired configuration is already applied")
		return
	}

	// discover and increment the latest running epoch
	epoch := a.latestEpoch() + 1
	// buffer aborts to prevent blocking on failing proxy
	abortCh := make(chan error, maxAborts)
	a.epochs[epoch] = a.desiredConfig
	a.abortCh[epoch] = abortCh
	a.currentConfig = a.desiredConfig
	go a.runWait(a.desiredConfig, epoch, abortCh)
}

// runWait runs the start-up command as a go routine and waits for it to finish
func (a *agent) runWait(config interface{}, epoch int, abortCh <-chan error) {
	log.Infof("Epoch %d starting", epoch)
	err := a.proxy.Run(config, epoch, abortCh)
	a.statusCh <- exitStatus{epoch: epoch, err: err}
}

// latestEpoch returns the latest epoch, or -1 if no epoch is running
func (a *agent) latestEpoch() int {
	epoch := -1
	for active := range a.epochs {
		if active > epoch {
			epoch = active
		}
	}
	return epoch
}

// abortAll sends abort error to all proxies
func (a *agent) abortAll() {
	for epoch, abortCh := range a.abortCh {
		log.Warnf("Aborting epoch %d...", epoch)
		abortCh <- errAbort
	}
	log.Warnf("Aborted all epochs")
}
