// Copyright 2017 Google Inc.
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

package aspect

import (
	"istio.io/mixer/pkg/config"
)

// APIBinding associates an aspect with an API method
type APIBinding struct {
	Aspect Manager
	Method config.APIMethod
}

// Name of all supported aspect kinds.
const (
	AccessLogKind = "access-logs"
	DenyKind      = "denials"
	ListKind      = "lists"
	LogKind       = "application-logs"
	MetricKind    = "metrics"
	QuotaKind     = "quotas"
)

// Inventory returns a manager inventory that contains
// all available aspect managers
func Inventory() []APIBinding {
	// Update the following list to add a new Aspect manager
	return []APIBinding{
		{NewDenyCheckerManager(), config.CheckMethod},
		{NewListCheckerManager(), config.CheckMethod},
		{NewLoggerManager(), config.ReportMethod},
		{NewAccessLoggerManager(), config.ReportMethod},
		{NewQuotaManager(), config.CheckMethod},
		{NewQuotaManager(), config.QuotaMethod},
	}
}
