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

package spybackend

import (
	"sync"

	adptModel "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
	checkTmpl "istio.io/istio/mixer/test/spyAdapter/template/check"
	quotaTmpl "istio.io/istio/mixer/test/spyAdapter/template/quota"
	reportTmpl "istio.io/istio/mixer/test/spyAdapter/template/report"
)

type (
	// Args specify captured requests and programmed behaviour
	Args struct {
		// manipulate the behavior of the backend.
		Behavior *Behavior

		// observed inputs by the backend
		Requests *Requests
	}

	// Behavior specifies programmed behaviour
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

		// check listEntry IBP
		HandleListEntryResult *adptModel.CheckResult
		HandleListEntryError  error

		// quota IBP
		HandleQuotaResult *adptModel.QuotaResult
		HandleQuotaError  error

		// sample quota IBP
		HandleSampleQuotaResult *adptModel.QuotaResult
		HandleSampleQuotaError  error

		// sample check IBP
		HandleSampleCheckResult *adptModel.CheckResult
		HandleSampleCheckError  error

		// sample report IBP
		HandleSampleReportResult *adptModel.ReportResult
		HandleSampleReportError  error
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

		samplecheckLock          sync.RWMutex
		HandleSampleCheckRequest []*checkTmpl.HandleSampleCheckRequest

		samplequotaLock          sync.RWMutex
		HandleSampleQuotaRequest []*quotaTmpl.HandleSampleQuotaRequest

		samplereportLock          sync.RWMutex
		HandleSampleReportRequest []*reportTmpl.HandleSampleReportRequest
	}
)

// DefaultArgs returns default arguments
func DefaultArgs() *Args {
	return &Args{
		Behavior: &Behavior{},
		Requests: &Requests{},
	}
}
