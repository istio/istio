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

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/cmd/pilot-agent/status/util"
)

// Probe for readiness.
type Probe struct {
	LocalHostAddr    string
	AdminPort        uint16
	ApplicationPorts []uint16
}

// Check executes the probe and returns an error if the probe fails.
func (p *Probe) Check() error {
	// First, check that Envoy has received a configuration update from Pilot.
	if err := p.checkUpdated(); err != nil {
		return err
	}

	// Envoy has received some configuration, make sure that configuration has been received for
	// all inbound ports.
	return p.checkInboundConfigured()
}

// checkApplicationPorts verifies that Envoy has received configuration for all ports exposed by the application container.
func (p *Probe) checkInboundConfigured() error {
	if len(p.ApplicationPorts) > 0 {
		listeningPorts, listeners, err := util.GetInboundListeningPorts(p.LocalHostAddr, p.AdminPort)
		if err != nil {
			return err
		}

		// Only those container ports exposed through the service receive a configuration from Pilot. Since we don't know
		// which ports are defined by the service, just ensure that at least one container port has a cluster/listener
		// configuration in Envoy. The CDS/LDS updates will contain everything, so just ensuring at least one port has
		// been configured should be sufficient.
		for _, appPort := range p.ApplicationPorts {
			if listeningPorts[appPort] {
				// Success - Envoy is configured.
				return nil
			}
			err = multierror.Append(err, fmt.Errorf("envoy missing listener for inbound application port: %d", appPort))
		}
		if err != nil {
			return multierror.Append(fmt.Errorf("failed checking application ports. listeners=%s", listeners), err)
		}
	}
	return nil
}

// checkUpdated checks to make sure updates have been received from Pilot
func (p *Probe) checkUpdated() error {
	s, err := util.GetStats(p.LocalHostAddr, p.AdminPort)
	if err != nil {
		return err
	}

	CDSUpdated := s.CDSUpdatesSuccess > 0 || s.CDSUpdatesRejection > 0
	LDSUpdated := s.LDSUpdatesSuccess > 0 || s.LDSUpdatesRejection > 0
	if CDSUpdated && LDSUpdated {
		return nil
	}

	return fmt.Errorf("config not received from Pilot (is Pilot running?): %s", s.String())
}
