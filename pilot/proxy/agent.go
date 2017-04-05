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
	"reflect"
	"time"

	"k8s.io/client-go/util/flowcontrol"

	"github.com/golang/glog"
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
// See https://lyft.github.io/envoy/docs/intro/arch_overview/hot_restart.html
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
	// ScheduleConfigUpdate sets the desired configuration for the proxy.  Agent
	// compares the current active configuration to the desired state and
	// initiates a restart if necessary. If the restart fails, the agent attempts
	// to retry with an exponential back-off.
	ScheduleConfigUpdate(config interface{})

	// Run starts the agent control loop and awaits for a signal on the input
	// channel to exit the loop.
	Run(stop <-chan struct{})
}

const (
	defaultDelay = 1 * time.Hour
)

// NewAgent creates a new proxy agent for the proxy start-up and clean-up functions.
func NewAgent(run func(interface{}, int) error, cleanup func(int),
	maxRetries int, initialInterval time.Duration) Agent {
	return &agent{
		run:      run,
		cleanup:  cleanup,
		epochs:   make(map[int]interface{}),
		configCh: make(chan interface{}),
		statusCh: make(chan exitStatus),
		retry: &retry{
			delay:           defaultDelay,
			defaultDelay:    defaultDelay,
			budget:          maxRetries,
			maxRetries:      maxRetries,
			initialInterval: initialInterval,
		},
	}
}

type retry struct {
	// delay between reconciliation adjusted by retries
	delay time.Duration

	// default delay is a long duration - reconciliation is a no-op in the regular case.
	// For permanent errors, reconciliation is still attempted once after the default delay.
	defaultDelay time.Duration

	// number of times to attempts left to retry applying the latest desired configuration
	budget int

	// maximum number of retries
	maxRetries int

	// delay between the first restart, from then on it is multiplied by a factor of 2
	// for each subsequent retry
	initialInterval time.Duration
}

type agent struct {
	// proxy start-up command
	run func(interface{}, int) error

	// proxy cleanup command
	cleanup func(int)

	// retry configuration
	retry *retry

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
}

type exitStatus struct {
	epoch int
	err   error
}

func (a *agent) ScheduleConfigUpdate(config interface{}) {
	a.configCh <- config
}

func (a *agent) Run(stop <-chan struct{}) {
	glog.V(2).Info("Starting proxy agent")

	// Throttle processing up to smoothed 1 qps with bursts up to 10 qps.
	// High QPS is needed to process messages on all channels.
	rateLimiter := flowcontrol.NewTokenBucketRateLimiter(float32(1), 10)

	for {
		rateLimiter.Accept()

		select {
		case config := <-a.configCh:
			if !reflect.DeepEqual(a.desiredConfig, config) {
				a.desiredConfig = config

				// reset retry budget if and only if the desired config changes
				a.retry.budget = a.retry.maxRetries
				a.retry.delay = a.retry.defaultDelay

				a.reconcile()
			}

		case status := <-a.statusCh:
			if status.err != nil {
				glog.V(2).Infof("Epoch %d terminated with an error: %v", status.epoch, status.err)
			} else {
				glog.V(2).Infof("Epoch %d exited normally", status.epoch)
			}

			// cleanup for the epoch
			a.cleanup(status.epoch)

			// delete epoch record and update current config
			delete(a.epochs, status.epoch)
			a.currentConfig = a.epochs[a.latestEpoch()]

			// schedule a retry for a transient error
			if status.err != nil && !reflect.DeepEqual(a.desiredConfig, a.currentConfig) {
				if a.retry.budget > 0 {
					a.retry.delay = a.retry.initialInterval * (1 << uint(a.retry.maxRetries-a.retry.budget))
					a.retry.budget = a.retry.budget - 1
				} else {
					glog.Warningf("Permanent error: budget exhausted trying to fulfill the desired configuration")
					// TODO: update proxy agent monitoring status about this error
					a.retry.delay = a.retry.defaultDelay
				}
			}

		case <-time.After(a.retry.delay):
			glog.V(2).Infof("Reconciling after delay %v (budget %d)", a.retry.delay, a.retry.budget)
			a.reconcile()

		case _, more := <-stop:
			if !more {
				// TODO: Proxy instances continue running, should be SIGed
				glog.V(2).Info("Agent terminating")
				return
			}
		}
	}
}

func (a *agent) reconcile() {
	// check that the config is current
	if reflect.DeepEqual(a.desiredConfig, a.currentConfig) {
		glog.V(2).Info("Desired configuration is already applied, resetting delay")
		a.retry.delay = a.retry.defaultDelay
		return
	}

	// discover and increment the latest running epoch
	epoch := a.latestEpoch() + 1
	a.epochs[epoch] = a.desiredConfig
	a.currentConfig = a.desiredConfig
	go a.waitForExit(a.desiredConfig, epoch)
}

// waitForExit runs the start-up command and waits for it to finish
func (a *agent) waitForExit(config interface{}, epoch int) {
	err := a.run(config, epoch)
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
