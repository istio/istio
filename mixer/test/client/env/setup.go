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

type TestSetup struct {
	t           *testing.T
	conf        string
	flags       string
	stress      bool
	faultInject bool
	noMixer     bool
	v2          *V2Conf
	ports       *Ports

	envoy   *Envoy
	mixer   *MixerServer
	backend *HttpServer
}

func NewTestSetup(name uint16, t *testing.T, conf string) *TestSetup {
	return &TestSetup{
		t:     t,
		conf:  conf,
		ports: NewPorts(name),
	}
}

func NewTestSetupV2(name uint16, t *testing.T) *TestSetup {
	return &TestSetup{
		t:     t,
		v2:    GetDefaultV2Conf(),
		ports: NewPorts(name),
	}
}

func (s *TestSetup) SetConf(conf string) {
	s.conf = conf
}

func (s *TestSetup) SetV2Conf() {
	s.v2 = GetDefaultV2Conf()
}

func (s *TestSetup) V2() *V2Conf {
	return s.v2
}

func (s *TestSetup) Ports() *Ports {
	return s.ports
}

func (s *TestSetup) SetMixerCheckReferenced(ref *mixerpb.ReferencedAttributes) {
	s.mixer.check_referenced = ref
}

func (s *TestSetup) SetMixerQuotaReferenced(ref *mixerpb.ReferencedAttributes) {
	s.mixer.quota_referenced = ref
}

func (s *TestSetup) SetMixerCheckStatus(status rpc.Status) {
	s.mixer.check.r_status = status
}

func (s *TestSetup) SetMixerQuotaStatus(status rpc.Status) {
	s.mixer.quota.r_status = status
}

func (s *TestSetup) SetMixerQuotaLimit(limit int64) {
	s.mixer.quota_limit = limit
}

func (s *TestSetup) GetMixerQuotaCount() int {
	return s.mixer.quota.count
}

func (s *TestSetup) SetFlags(flags string) {
	s.flags = flags
}

func (s *TestSetup) SetStress(stress bool) {
	s.stress = stress
}

func (s *TestSetup) SetNoMixer(no bool) {
	s.noMixer = no
}

func (s *TestSetup) SetFaultInject(f bool) {
	s.faultInject = f
}

func (s *TestSetup) SetUp() error {
	var err error
	s.envoy, err = NewEnvoy(s.conf, s.flags, s.stress, s.faultInject, s.v2, s.ports)
	if err != nil {
		log.Printf("unable to create Envoy %v", err)
	} else {
		s.envoy.Start()
	}

	if !s.noMixer {
		s.mixer, err = NewMixerServer(s.ports.MixerPort, s.stress)
		if err != nil {
			log.Printf("unable to create mixer server %v", err)
		} else {
			s.mixer.Start()
		}
	}

	s.backend, err = NewHttpServer(s.ports.BackendPort)
	if err != nil {
		log.Printf("unable to create HTTP server %v", err)
	} else {
		s.backend.Start()
	}
	return err
}

func (s *TestSetup) TearDown() {
	s.envoy.Stop()
	if s.mixer != nil {
		s.mixer.Stop()
	}
	s.backend.Stop()
}

func (s *TestSetup) ReStartEnvoy() {
	s.envoy.Stop()
	var err error
	s.envoy, err = NewEnvoy(s.conf, s.flags, s.stress, s.faultInject, s.v2, s.ports)
	if err != nil {
		s.t.Errorf("unable to re-start Envoy %v", err)
	} else {
		s.envoy.Start()
	}
}

func (s *TestSetup) VerifyCheckCount(tag string, expected int) {
	if s.mixer.check.count != expected {
		s.t.Fatalf("%s check count doesn't match: %v\n, expected: %+v",
			tag, s.mixer.check.count, expected)
	}
}

func (s *TestSetup) VerifyReportCount(tag string, expected int) {
	if s.mixer.report.count != expected {
		s.t.Fatalf("%s report count doesn't match: %v\n, expected: %+v",
			tag, s.mixer.report.count, expected)
	}
}

func (s *TestSetup) VerifyCheck(tag string, result string) {
	bag := <-s.mixer.check.ch
	if err := Verify(bag, result); err != nil {
		s.t.Fatalf("Failed to verify %s check: %v\n, Attributes: %+v",
			tag, err, bag)
	}
}

func (s *TestSetup) VerifyReport(tag string, result string) {
	bag := <-s.mixer.report.ch
	if err := Verify(bag, result); err != nil {
		s.t.Fatalf("Failed to verify %s report: %v\n, Attributes: %+v",
			tag, err, bag)
	}
}

func (s *TestSetup) VerifyQuota(tag string, name string, amount int64) {
	_ = <-s.mixer.quota.ch
	if s.mixer.qma.Quota != name {
		s.t.Fatalf("Failed to verify %s quota name: %v, expected: %v\n",
			tag, s.mixer.qma.Quota, name)
	}
	if s.mixer.qma.Amount != amount {
		s.t.Fatalf("Failed to verify %s quota amount: %v, expected: %v\n",
			tag, s.mixer.qma.Amount, amount)
	}
}

func (s *TestSetup) DrainMixerAllChannels() {
	go func() {
		for true {
			_ = <-s.mixer.check.ch
		}
	}()
	go func() {
		for true {
			_ = <-s.mixer.report.ch
		}
	}()
	go func() {
		for true {
			_ = <-s.mixer.quota.ch
		}
	}()
}
