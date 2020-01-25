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

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"

	"istio.io/istio/pilot/cmd/pilot-agent/status/util"
	"istio.io/istio/pilot/pkg/model"
)

// Probe for readiness.
type Probe struct {
	LocalHostAddr       string
	NodeType            model.NodeType
	AdminPort           uint16
	receivedFirstUpdate bool
}

// Check executes the probe and returns an error if the probe fails.
func (p *Probe) Check() error {
	// First, check that Envoy has received a configuration update from Pilot.
	if err := p.checkConfigStatus(); err != nil {
		return err
	}
	return p.checkServerState()
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

// checkServerState checks to ensure that Envoy is in the READY state
func (p *Probe) checkServerState() error {
	state, err := util.GetServerState(p.LocalHostAddr, p.AdminPort)
	if err != nil {
		return fmt.Errorf("failed to get server info: %v", err)
	}

	if state != nil && admin.ServerInfo_State(*state) != admin.ServerInfo_LIVE {
		return fmt.Errorf("server is not live, current state is: %v", admin.ServerInfo_State(*state).String())
	}

	return nil
}
