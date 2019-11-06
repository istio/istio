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
	"time"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/cmd/pilot-agent/status/util"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/pkg/log"
)

// Probe for readiness.
type Probe struct {
	LocalHostAddr  string
	ProxyIP        string
	NodeType       model.NodeType
	AdminPort      uint16
	listenersBound bool
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
			serverState: uint64(admin.ServerInfo_PRE_INITIALIZING),
		}
	}
	// First, check that Envoy has received a configuration update from Pilot.
	if err := p.checkConfigStatus(); err != nil {
		return err
	}

	return p.isEnvoyReady()
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
	} else {
		log.Warnf("config status timed out. using last known values cds status: %d,lds status: %d", p.lastKnownState.versionStats.CDSVersion,
			p.lastKnownState.versionStats.LDSVersion)
	}

	// Envoy seems to not updatig cds version (need to confirm) when partial rejection happens.
	// Till that is fixed, we should treat LDS success as readiness success as LDS happens last.
	if p.lastKnownState.versionStats.LDSVersion > 0 {
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
	} else {
		log.Warnf("config server state timed out. using last known state %s", admin.ServerInfo_State(p.lastKnownState.serverState).String())
	}

	if admin.ServerInfo_State(p.lastKnownState.serverState) != admin.ServerInfo_LIVE {
		return fmt.Errorf("server is not live, current state is: %s", admin.ServerInfo_State(p.lastKnownState.serverState).String())
	}

	return nil
}

func (p *Probe) isEnvoyReady() error {
	if se := p.checkServerState(); se != nil {
		return se
	}
	if p.NodeType == model.Router {
		return nil
	}
	if pe := p.pingVirtualListeners(); pe != nil {
		return pe
	}
	return nil
}

// pingVirtualListeners checks to ensure that Envoy is actually listenening on the port.
func (p *Probe) pingVirtualListeners() error {
	// It is OK to cache this because, for hot restarts the initial listener update check
	// will ensure listeners are received and drain + parent shutdown wait times will ensure it is bound
	// before child Envoy takes traffic.
	if len(p.ProxyIP) == 0 || p.listenersBound {
		return nil
	}

	// Check if traffic capture ports are actually listening.
	vports, err := util.GetVirtualListenerPorts(p.LocalHostAddr, p.AdminPort)
	if err != nil {
		return err
	}

	for _, vport := range vports {
		con, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", p.ProxyIP, vport), time.Second*1)
		if con != nil {
			con.Close()
		}
		if err != nil {
			return fmt.Errorf("listener on address %d is still not listening: %v", vport, err)
		}
	}

	p.listenersBound = true

	return nil
}

func isTimeout(err error) bool {
	if err, ok := err.(net.Error); ok && err.Timeout() {
		return true
	}
	return false
}
