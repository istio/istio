// Copyright Istio Authors
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

package spybackend

import (
	"sync"
	"time"

	adptModel "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
	apaTmpl "istio.io/istio/mixer/test/spyAdapter/template/apa"
	checkTmpl "istio.io/istio/mixer/test/spyAdapter/template/check"
	checkoutputTmpl "istio.io/istio/mixer/test/spyAdapter/template/checkoutput"
	quotaTmpl "istio.io/istio/mixer/test/spyAdapter/template/quota"
	reportTmpl "istio.io/istio/mixer/test/spyAdapter/template/report"
)

type (
	// Args specify captured requests and programmed behavior
	Args struct {
		// manipulate the behavior of the backend.
		Behavior *Behavior

		// observed inputs by the backend
		Requests *Requests
	}

	// Behavior specifies programmed behavior
	Behavior struct {
		ValidateResponse *adptModel.ValidateResponse
		ValidateError    error

		CreateSessionResponse *adptModel.CreateSessionResponse
		CreateSessionError    error

		CloseSessionResponse *adptModel.CloseSessionResponse
		CloseSessionError    error

		// report metric IBP
		HandleMetricResult *adptModel.ReportResult
		HandleMetricError  error
		HandleMetricSleep  time.Duration

		// check listEntry IBP
		HandleListEntryResult *adptModel.CheckResult
		HandleListEntryError  error
		HandleListEntrySleep  time.Duration

		// quota IBP
		HandleQuotaResult *adptModel.QuotaResult
		HandleQuotaError  error
		HandleQuotaSleep  time.Duration

		// sample quota IBP
		HandleSampleQuotaResult *adptModel.QuotaResult
		HandleSampleQuotaError  error
		HandleSampleQuotaSleep  time.Duration

		// sample check IBP
		HandleSampleCheckResult *adptModel.CheckResult
		HandleSampleCheckError  error
		HandleCheckOutput       *checkoutputTmpl.OutputMsg
		HandleSampleCheckSleep  time.Duration

		// sample report IBP
		HandleSampleReportResult *adptModel.ReportResult
		HandleSampleReportError  error
		HandleSampleReportSleep  time.Duration

		// sample APA IBP
		HandleSampleApaResult *apaTmpl.OutputMsg
		HandleSampleApaError  error
		HandleSampleApaSleep  time.Duration

		// Auth
		HeaderKey                string
		HeaderToken              string
		CredsPath                string
		KeyPath                  string
		CertPath                 string
		InsecureSkipVerification bool
		RequireTLS               bool
		RequireMTls              bool
	}

	// Requests record captured requests by the spy
	Requests struct {
		ValidateRequest []*adptModel.ValidateRequest

		CreateSessionRequest []*adptModel.CreateSessionRequest

		CloseSessionRequest []*adptModel.CloseSessionRequest

		metricLock          sync.RWMutex
		HandleMetricRequest []*metric.HandleMetricRequest

		listentryLock          sync.RWMutex
		HandleListEntryRequest []*listentry.HandleListEntryRequest

		quotaLock          sync.RWMutex
		HandleQuotaRequest []*quota.HandleQuotaRequest

		HandleSampleCheckRequest []*checkTmpl.HandleSampleCheckRequest

		HandleSampleQuotaRequest []*quotaTmpl.HandleSampleQuotaRequest

		HandleSampleReportRequest []*reportTmpl.HandleSampleReportRequest

		HandleSampleApaRequest []*apaTmpl.HandleSampleApaRequest
	}
)

// DefaultArgs returns default arguments
func DefaultArgs() *Args {
	return &Args{
		Behavior: &Behavior{},
		Requests: &Requests{},
	}
}
