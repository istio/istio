// Copyright 2018 Istio Authors
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
	"net"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/cmd/pilot-agent/status/util"
	"istio.io/istio/pilot/pkg/model"
)

// Probe for readiness.
type Probe struct {
	LocalHostAddr  string
	NodeType       model.NodeType
	AdminPort      uint16
	lastKnownState *probeState
}

type probeState struct {
	serverState  uint64
	versionStats util.Stats
}

// Check executes the probe and returns an error if the probe fails.
func (p *Probe) Check() error {
	// Initialize the ServerState to 2 (PRE_INITIALIZING) because 0 indicates LIVE.
	if p.lastKnownState == nil {
		p.lastKnownState = &probeState{
			serverState: 2,
		}
	}
	// First, check that Envoy has received a configuration update from Pilot.
	if err := p.checkConfigStatus(); err != nil {
		return err
	}

	return p.checkServerState()
}

// checkConfigStatus checks to make sure initial configs have been received from Pilot.
func (p *Probe) checkConfigStatus() error {
	s, err := util.GetVersionStats(p.LocalHostAddr, p.AdminPort)

	// If there is timeout, Envoy may be busy processing the config - just return the last known state.
	if err != nil && !isTimeout(err) {
		return multierror.Prefix(err, "failed retrieving Envoy stats:")
	}
	if err == nil {
		p.lastKnownState.versionStats.CDSVersion = s.CDSVersion
		p.lastKnownState.versionStats.LDSVersion = s.LDSVersion
	}

	if p.lastKnownState.versionStats.CDSVersion > 0 && p.lastKnownState.versionStats.LDSVersion > 0 {
		return nil
	}

	return fmt.Errorf("config not received from Pilot (is Pilot running?) %s", p.lastKnownState.versionStats.String())
}

// checkServerState checks to ensure that Envoy is in the READY state
func (p *Probe) checkServerState() error {
	state, err := util.GetServerState(p.LocalHostAddr, p.AdminPort)

	// If there is timeout, Envoy may be busy processing the config - just return the last known state.
	if err != nil && !isTimeout(err) {
		return multierror.Prefix(err, "failed retrieving Envoy server state stat:")
	}

	if err == nil {
		p.lastKnownState.serverState = *state
	}

	if admin.ServerInfo_State(p.lastKnownState.serverState) != admin.ServerInfo_LIVE {
		return fmt.Errorf("server is not live, current state is: %s", admin.ServerInfo_State(*state).String())
	}

	return nil
}

func isTimeout(err error) bool {
	if err, ok := err.(net.Error); ok && err.Timeout() {
		return true
	}
	return false
}
