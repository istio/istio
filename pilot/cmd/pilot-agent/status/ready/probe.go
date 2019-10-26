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

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"

	"istio.io/istio/pilot/cmd/pilot-agent/status/util"
	"istio.io/istio/pilot/pkg/model"
)

// Probe for readiness.
type Probe struct {
	ApplicationPorts []uint16
	LocalHostAddr    string
	NodeType         model.NodeType
	AdminPort        uint16
}

// Check executes the probe and returns an error if the probe fails.
func (p *Probe) Check() error {
	// First, check that Envoy has received a configuration update from Pilot.
	if err := p.checkUpdated(); err != nil {
		return err
	}

	return p.checkServerInfo()
}

// checkUpdated checks to make sure updates have been received from Pilot
func (p *Probe) checkUpdated() error {
	s, err := util.GetVersionStats(p.LocalHostAddr, p.AdminPort)
	if err != nil {
		return err
	}

	if s.CDSVersion > 0 && s.LDSVersion > 0 {
		return nil
	}

	return fmt.Errorf("config not received from Pilot (is Pilot running?) %s", s.String())
}

// checkServerInfo checks to ensure that Envoy is in the READY state
func (p *Probe) checkServerInfo() error {
	state, err := util.GetServerState(p.LocalHostAddr, p.AdminPort)
	if err != nil {
		return fmt.Errorf("failed to get server info: %v", err)
	}

	if admin.ServerInfo_State(*state) != admin.ServerInfo_LIVE {
		return fmt.Errorf("server is not live, current state is: %s", admin.ServerInfo_State(*state).String())
	}

	return nil
}
