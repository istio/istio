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
	"time"

	"istio.io/pkg/log"
)

var errAbort = errors.New("epoch aborted")

const errOutOfMemory = "signal: killed"

// NewAgent creates a new proxy agent for the proxy start-up and clean-up functions.
func NewAgent(proxy Proxy, terminationDrainDuration time.Duration) *Agent {
	return &Agent{
		proxy:                    proxy,
		statusCh:                 make(chan exitStatus),
		abortCh:                  make(chan error, 1),
		terminationDrainDuration: terminationDrainDuration,
	}
}

// Proxy defines command interface for a proxy
type Proxy interface {

	// Run command for a config, epoch, and abort channel
	Run(int, <-chan error) error

	// Drains the current epoch.
	Drain() error

	// Cleanup command for an epoch
	Cleanup(int)
}

type Agent struct {
	// proxy commands
	proxy Proxy

	// channel for proxy exit notifications
	statusCh chan exitStatus

	abortCh chan error

	// time to allow for the proxy to drain before terminating all remaining proxy processes
	terminationDrainDuration time.Duration
}

type exitStatus struct {
	epoch int
	err   error
}

// Run starts the envoy and waits until it terminates.
func (a *Agent) Run(ctx context.Context) error {
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
		return nil
	case <-ctx.Done():
		a.terminate()
		log.Info("Agent has successfully terminated")
		return nil
	}
}

func (a *Agent) terminate() {
	log.Infof("Agent draining Proxy")
	e := a.proxy.Drain()
	if e != nil {
		log.Warnf("Error in invoking drain listeners endpoint %v", e)
	}
	log.Infof("Graceful termination period is %v, starting...", a.terminationDrainDuration)
	time.Sleep(a.terminationDrainDuration)
	log.Infof("Graceful termination period complete, terminating remaining proxies.")
	a.abortCh <- errAbort
	log.Warnf("Aborted all epochs")
}

// runWait runs the start-up command as a go routine and waits for it to finish
func (a *Agent) runWait(epoch int, abortCh <-chan error) {
	log.Infof("Epoch %d starting", epoch)
	err := a.proxy.Run(epoch, abortCh)
	a.proxy.Cleanup(epoch)
	a.statusCh <- exitStatus{epoch: epoch, err: err}
}
