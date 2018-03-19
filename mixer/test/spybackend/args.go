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
	adptModel "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
)

type (
	args struct {
		// manipulate the behavior of the backend.
		behavior *behavior

		// observed inputs by the backend
		requests *requests
	}

	behavior struct {
		validateResponse *adptModel.ValidateResponse
		validateError    error

		createSessionResponse *adptModel.CreateSessionResponse
		createSessionError    error

		closeSessionResponse *adptModel.CloseSessionResponse
		closeSessionError    error

		// report metric IBP
		handleMetricResult *adptModel.ReportResult
		handleMetricError  error

		// check listEntry IBP
		handleListEntryResult *adptModel.CheckResult
		handleListEntryError  error

		// quota IBP
		handleQuotaResult *adptModel.QuotaResult
		handleQuotaError  error
	}

	requests struct {
		validateRequest []*adptModel.ValidateRequest

		createSessionRequest []*adptModel.CreateSessionRequest

		closeSessionRequest []*adptModel.CloseSessionRequest

		handleMetricRequest    []*metric.HandleMetricRequest
		handleListEntryRequest []*listentry.HandleListEntryRequest
		handleQuotaRequest     []*quota.HandleQuotaRequest
	}
)

// nolint:deadcode
func defaultArgs() *args {
	return &args{
		behavior: &behavior{},
		requests: &requests{},
	}
}
