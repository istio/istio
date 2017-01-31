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

package adapter

// Registrar is used by adapters to register aspect builders.
type Registrar interface {
	// RegisterListChecker registers a new ListChecker builder.
	RegisterListChecker(ListCheckerBuilder)

	// RegisterDenyChecker registers a new DenyChecker builder.
	RegisterDenyChecker(DenyCheckerBuilder)

	// RegisterLogger registers a new Logger builder.
	RegisterLogger(LoggerBuilder)

	// RegisterAccessLogger registers a new AccessLogger builder.
	RegisterAccessLogger(AccessLoggerBuilder)

	// RegisterQuota registers a new Quota builder.
	RegisterQuota(QuotaBuilder)

	// RegisterMetrics registers a new Metrics builder.
	RegisterMetrics(MetricsBuilder)
}

// RegisterFn is a function the mixer invokes to trigger adapters to register
// their aspect builders. It must succeed or panic().
type RegisterFn func(Registrar)
