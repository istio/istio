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

package test

import (
	"fmt"
	"testing"
	"time"
)

const checkOnlyFlags = `
                   "mixer_check": "on",
`

const reportOnlyFlags = `
                   "mixer_report": "on",
`

func TestCheckReportDisable(t *testing.T) {
	s := &TestSetup{
		t:    t,
		conf: basicConfig + "," + disableReportBatch,
	}
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", ClientProxyPort)

	tag := "Both Check and Report"
	if _, _, err := HTTPGet(url); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	// Even report batch is disabled, but it is better to wait
	// since sending batch is after request is completed.
	time.Sleep(1 * time.Second)
	// Send both check and report
	s.VerifyCheckCount(tag, 1)
	s.VerifyReportCount(tag, 1)

	var err error
	// stop and start a new envoy config
	s.envoy.Stop()
	s.envoy, err = NewEnvoy(s.conf, checkOnlyFlags, s.stress)
	if err != nil {
		t.Errorf("unable to re-create Envoy %v", err)
	} else {
		s.envoy.Start()
	}

	// wait for 2 second to wait for envoy to come up
	time.Sleep(2 * time.Second)
	tag = "Check Only"
	if _, _, err := HTTPGet(url); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	time.Sleep(1 * time.Second)
	// Only send check, not report.
	s.VerifyCheckCount(tag, 2)
	s.VerifyReportCount(tag, 1)

	// stop and start a new envoy config
	s.envoy.Stop()
	s.envoy, err = NewEnvoy(s.conf, reportOnlyFlags, s.stress)
	if err != nil {
		t.Errorf("unable to re-create Envoy %v", err)
	} else {
		s.envoy.Start()
	}

	// wait for 2 second to wait for envoy to come up
	time.Sleep(2 * time.Second)
	tag = "Report Only"
	if _, _, err := HTTPGet(url); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	time.Sleep(1 * time.Second)
	// Only send report, not check.
	s.VerifyCheckCount(tag, 2)
	s.VerifyReportCount(tag, 2)
}
