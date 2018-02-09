// Copyright 2017 Istio Authors. All Rights Reserved.
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

package env

import (
	"log"
	"testing"

	mixerpb "istio.io/api/mixer/v1"
	rpc "istio.io/gogo-genproto/googleapis/google/rpc"
)

// TestSetup store data for a test.
type TestSetup struct {
	t           *testing.T
	stress      bool
	faultInject bool
	noMixer     bool
	v2          *V2Conf
	ports       *Ports

	envoy   *Envoy
	mixer   *MixerServer
	backend *HTTPServer
}

// "name" has to be defined in ports.go
func NewTestSetup(name uint16, t *testing.T) *TestSetup {
	return &TestSetup{
		t:     t,
		v2:    GetDefaultV2Conf(),
		ports: NewPorts(name),
	}
}

// V2 get v2 config
func (s *TestSetup) V2() *V2Conf {
	return s.v2
}

// Ports get ports object
func (s *TestSetup) Ports() *Ports {
	return s.ports
}

// SetMixerCheckReferenced set Referenced in mocked Check response
func (s *TestSetup) SetMixerCheckReferenced(ref *mixerpb.ReferencedAttributes) {
	s.mixer.checkReferenced = ref
}

// SetMixerQuotaReferenced set Referenced in mocked Quota response
func (s *TestSetup) SetMixerQuotaReferenced(ref *mixerpb.ReferencedAttributes) {
	s.mixer.quotaReferenced = ref
}

// SetMixerCheckStatus set Status in mocked Check response
func (s *TestSetup) SetMixerCheckStatus(status rpc.Status) {
	s.mixer.check.rStatus = status
}

// SetMixerQuotaStatus set Status in mocked Quota response
func (s *TestSetup) SetMixerQuotaStatus(status rpc.Status) {
	s.mixer.quota.rStatus = status
}

// SetMixerQuotaLimit set mock quota limit
func (s *TestSetup) SetMixerQuotaLimit(limit int64) {
	s.mixer.quotaLimit = limit
}

// GetMixerQuotaCount get the number of Quota calls.
func (s *TestSetup) GetMixerQuotaCount() int {
	return s.mixer.quota.count
}

// SetStress set the stress flag
func (s *TestSetup) SetStress(stress bool) {
	s.stress = stress
}

// SetNoMixer set NoMixer flag
func (s *TestSetup) SetNoMixer(no bool) {
	s.noMixer = no
}

// SetFaultInject set FaultInject flag
func (s *TestSetup) SetFaultInject(f bool) {
	s.faultInject = f
}

// SetUp setups Envoy, Mixer, and Backend server for test.
func (s *TestSetup) SetUp() error {
	var err error
	s.envoy, err = NewEnvoy(s.stress, s.faultInject, s.v2, s.ports)
	if err != nil {
		log.Printf("unable to create Envoy %v", err)
	} else {
		_ = s.envoy.Start()
	}

	if !s.noMixer {
		s.mixer, err = NewMixerServer(s.ports.MixerPort, s.stress)
		if err != nil {
			log.Printf("unable to create mixer server %v", err)
		} else {
			s.mixer.Start()
		}
	}

	s.backend, err = NewHTTPServer(s.ports.BackendPort)
	if err != nil {
		log.Printf("unable to create HTTP server %v", err)
	} else {
		s.backend.Start()
	}
	return err
}

// TearDown shutdown the servers.
func (s *TestSetup) TearDown() {
	_ = s.envoy.Stop()
	if s.mixer != nil {
		s.mixer.Stop()
	}
	s.backend.Stop()
}

// ReStartEnvoy restarts Envoy
func (s *TestSetup) ReStartEnvoy() {
	_ = s.envoy.Stop()
	var err error
	s.envoy, err = NewEnvoy(s.stress, s.faultInject, s.v2, s.ports)
	if err != nil {
		s.t.Errorf("unable to re-start Envoy %v", err)
	} else {
		_ = s.envoy.Start()
	}
}

// VerifyCheckCount verifies the number of Check calls.
func (s *TestSetup) VerifyCheckCount(tag string, expected int) {
	if s.mixer.check.count != expected {
		s.t.Fatalf("%s check count doesn't match: %v\n, expected: %+v",
			tag, s.mixer.check.count, expected)
	}
}

// VerifyReportCount verifies the number of Report calls.
func (s *TestSetup) VerifyReportCount(tag string, expected int) {
	if s.mixer.report.count != expected {
		s.t.Fatalf("%s report count doesn't match: %v\n, expected: %+v",
			tag, s.mixer.report.count, expected)
	}
}

// VerifyCheck verifies Check request data.
func (s *TestSetup) VerifyCheck(tag string, result string) {
	bag := <-s.mixer.check.ch
	if err := Verify(bag, result); err != nil {
		s.t.Fatalf("Failed to verify %s check: %v\n, Attributes: %+v",
			tag, err, bag)
	}
}

// VerifyReport verifies Report request data.
func (s *TestSetup) VerifyReport(tag string, result string) {
	bag := <-s.mixer.report.ch
	if err := Verify(bag, result); err != nil {
		s.t.Fatalf("Failed to verify %s report: %v\n, Attributes: %+v",
			tag, err, bag)
	}
}

// VerifyQuota verified Quota request data.
func (s *TestSetup) VerifyQuota(tag string, name string, amount int64) {
	<-s.mixer.quota.ch
	if s.mixer.qma.Quota != name {
		s.t.Fatalf("Failed to verify %s quota name: %v, expected: %v\n",
			tag, s.mixer.qma.Quota, name)
	}
	if s.mixer.qma.Amount != amount {
		s.t.Fatalf("Failed to verify %s quota amount: %v, expected: %v\n",
			tag, s.mixer.qma.Amount, amount)
	}
}

// DrainMixerAllChannels drain all channels
func (s *TestSetup) DrainMixerAllChannels() {
	go func() {
		for {
			<-s.mixer.check.ch
		}
	}()
	go func() {
		for {
			<-s.mixer.report.ch
		}
	}()
	go func() {
		for {
			<-s.mixer.quota.ch
		}
	}()
}
