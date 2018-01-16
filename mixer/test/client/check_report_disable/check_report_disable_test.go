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

package checkReportDisable

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/mixer/test/client/env"
)

const checkOnlyFlags = `
                   "mixer_check": "on",
`

const reportOnlyFlags = `
                   "mixer_report": "on",
`

func TestCheckReportDisable(t *testing.T) {
	s := env.NewTestSetup(
		env.CheckReportDisableTest,
		t,
		env.BasicConfig+","+env.DisableCheckCache+","+env.DisableReportBatch)
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().ClientProxyPort)

	tag := "Both Check and Report v1"
	if _, _, err := env.HTTPGet(url); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	// Even report batch is disabled, but it is better to wait
	// since sending batch is after request is completed.
	time.Sleep(1 * time.Second)
	// Send both check and report
	s.VerifyCheckCount(tag, 1)
	s.VerifyReportCount(tag, 1)

	// stop and start a new envoy config
	s.SetFlags(checkOnlyFlags)
	s.ReStartEnvoy()

	tag = "Check Only v1"
	if _, _, err := env.HTTPGet(url); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	// Wait for Report
	time.Sleep(1 * time.Second)
	// Only send check, not report.
	s.VerifyCheckCount(tag, 2)
	s.VerifyReportCount(tag, 1)

	// stop and start a new envoy config
	s.SetFlags(reportOnlyFlags)
	s.ReStartEnvoy()

	// wait for 2 second to wait for envoy to come up
	tag = "Report Only v1"
	if _, _, err := env.HTTPGet(url); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	// Wait for Report
	time.Sleep(1 * time.Second)
	// Only send report, not check.
	s.VerifyCheckCount(tag, 2)
	s.VerifyReportCount(tag, 2)

	//
	// Use V2 config
	//

	s.SetV2Conf()
	// Disable all cache.
	env.DisableClientCache(s.V2().HTTPServerConf, true, true, true)
	s.ReStartEnvoy()

	tag = "Both Check and Report"
	if _, _, err := env.HTTPGet(url); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	// Even report batch is disabled, but it is better to wait
	// since sending batch is after request is completed.
	time.Sleep(1 * time.Second)
	// Send both check and report
	s.VerifyCheckCount(tag, 3)
	s.VerifyReportCount(tag, 3)

	// stop and start a new envoy config
	// Check enabled, Report disabled
	env.DisableHTTPCheckReport(s.V2().HTTPServerConf, false, true)
	s.ReStartEnvoy()

	tag = "Check Only"
	if _, _, err := env.HTTPGet(url); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	// Wait for Report
	time.Sleep(1 * time.Second)
	// Only send check, not report.
	s.VerifyCheckCount(tag, 4)
	s.VerifyReportCount(tag, 3)

	// Check disabled, Report enabled
	env.DisableHTTPCheckReport(s.V2().HTTPServerConf, true, false)
	s.ReStartEnvoy()

	tag = "Report Only"
	if _, _, err := env.HTTPGet(url); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	// Wait for Report
	time.Sleep(1 * time.Second)
	// Only send report, not check.
	s.VerifyCheckCount(tag, 4)
	s.VerifyReportCount(tag, 4)
}
