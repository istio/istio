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

package ready

import (
	"fmt"
	"time"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"

	"istio.io/istio/pilot/cmd/pilot-agent/status/util"
	"istio.io/istio/pilot/pkg/model"
)

var (
	// readinessTTL is the TTL for cached readiness values.
	readinessTTL = 60 * time.Second
)

// Probe for readiness.
type Probe struct {
	LocalHostAddr       string
	NodeType            model.NodeType
	AdminPort           uint16
	receivedFirstUpdate bool
	lastUpdateTime      time.Time
	// Indicates that Envoy is ready atleast once so that we can cache and reuse that probe.
	// If after TTL, Envoy becomes unready, we will reset this flag so that we continuously
	// check Envoy till it becomes ready.
	atleastOnceReady bool
	readyError       error
}

// Check executes the probe and returns an error if the probe fails.
func (p *Probe) Check() error {
	// First, check that Envoy has received a configuration update from Pilot.
	if err := p.checkConfigStatus(); err != nil {
		return err
	}
	return p.isEnvoyReady()
}

// checkConfigStatus checks to make sure initial configs have been received from Pilot.
func (p *Probe) checkConfigStatus() error {
	if p.receivedFirstUpdate {
		return nil
	}

	s, err := util.GetUpdateStatusStats(p.LocalHostAddr, p.AdminPort)
	if err != nil {
		return err
	}

	CDSUpdated := s.CDSUpdatesSuccess > 0
	LDSUpdated := s.LDSUpdatesSuccess > 0
	if CDSUpdated && LDSUpdated {
		p.receivedFirstUpdate = true
		return nil
	}

	return fmt.Errorf("config not received from Pilot (is Pilot running?): %s", s.String())
}

// isEnvoyReady checks to ensure that Envoy is in the LIVE state and workers have started.
func (p *Probe) isEnvoyReady() error {
	// Execute the stats query on Envoy if atleast readinessTTL has expired or
	// Envoy is not ready at least once.  After Envoy is ready for the first time,
	// we return cached value to avoid frequent executions of stats query till
	// cached TTL is reached.
	if !p.atleastOnceReady || time.Since(p.lastUpdateTime) >= readinessTTL {
		p.readyError = checkEnvoyStats(p.LocalHostAddr, p.AdminPort)
		if p.readyError == nil && !p.atleastOnceReady {
			p.atleastOnceReady = true
		}
		// If readiness fails, we should keep checking.
		if p.readyError != nil {
			p.atleastOnceReady = false
		}
		p.lastUpdateTime = time.Now()
	}
	return p.readyError
}

// checkEnvoyStats actually executes the Stats Query on Envoy admin endpoint.
func checkEnvoyStats(host string, port uint16) error {
	state, ws, err := util.GetReadinessStats(host, port)
	if err != nil {
		return fmt.Errorf("failed to get readiness stats: %v", err)
	}

	if state != nil && admin.ServerInfo_State(*state) != admin.ServerInfo_LIVE {
		return fmt.Errorf("server is not live, current state is: %v", admin.ServerInfo_State(*state).String())
	}

	if !ws {
		return fmt.Errorf("workers have not yet started")
	}

	return nil
}
