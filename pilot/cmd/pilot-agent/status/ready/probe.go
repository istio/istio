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
	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/cmd/pilot-agent/status/util"
	"istio.io/istio/pilot/pkg/model"
)

// Probe for readiness.
type Probe struct {
	ApplicationPorts    []uint16
	LocalHostAddr       string
	NodeType            model.NodeType
	AdminPort           uint16
	receivedFirstUpdate bool
}

// Check executes the probe and returns an error if the probe fails.
func (p *Probe) Check() error {
	// First, check that Envoy has received a configuration update from Pilot.
	if err := p.checkUpdated(); err != nil {
		return err
	}

	// Envoy has received some listener configuration, make sure that configuration has been received for
	// any of the inbound ports.
	if p.NodeType == model.Router {
		if err := p.checkInboundConfigured(); err != nil {
			return err
		}
	}

	return p.checkServerInfo()
}

// checkApplicationPorts verifies that Envoy has received configuration for all ports exposed by the application container.
// Notes it is used only by envoy in Router mode.
func (p *Probe) checkInboundConfigured() error {
	if len(p.ApplicationPorts) > 0 {
		listeningPorts, listeners, err := util.GetInboundListeningPorts(p.LocalHostAddr, p.AdminPort, p.NodeType)
		if err != nil {
			return err
		}

		// The CDS/LDS updates will contain everything, so just ensuring at least one port has been configured
		// should be sufficient. The full check is mainly to provide a full inspection.
		for _, appPort := range p.ApplicationPorts {
			if !listeningPorts[appPort] {
				err = multierror.Append(err, fmt.Errorf("envoy missing listener for inbound application port: %d", appPort))
			}
		}
		if err != nil {
			return multierror.Append(fmt.Errorf("failed checking application ports. listeners=%s", listeners), err)
		}
	}
	return nil
}

// checkUpdated checks to make sure updates have been received from Pilot
func (p *Probe) checkUpdated() error {
	if p.receivedFirstUpdate {
		return nil
	}

	s, err := util.GetStats(p.LocalHostAddr, p.AdminPort)
	if err != nil {
		return err
	}

	CDSUpdated := s.CDSUpdatesSuccess > 0 || s.CDSUpdatesRejection > 0
	LDSUpdated := s.LDSUpdatesSuccess > 0 || s.LDSUpdatesRejection > 0
	if CDSUpdated && LDSUpdated {
		p.receivedFirstUpdate = true
		return nil
	}

	return fmt.Errorf("config not received from Pilot (is Pilot running?): %s", s.String())
}

// checkServerInfo checks to ensure that Envoy is in the READY state
func (p *Probe) checkServerInfo() error {
	info, err := util.GetServerInfo(p.LocalHostAddr, p.AdminPort)
	if err != nil {
		return fmt.Errorf("failed to get server info: %v", err)
	}

	if info.GetState() != admin.ServerInfo_LIVE {
		return fmt.Errorf("server is not live, current state is: %v", info.GetState().String())
	}

	return nil
}
