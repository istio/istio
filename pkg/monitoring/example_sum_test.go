// Copyright 2019 Istio Authors
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

package monitoring_test

import "istio.io/istio/pkg/monitoring"

var (
	protocol = monitoring.CreateLabel("protocol")

	requests = monitoring.NewSum(
		"requests_total",
		"Number of requests handled, by protocol",
	)
)

func ExampleNewSum() {
	// increment on every http request
	requests.With(protocol.Value("http")).Increment()

	// count gRPC requests double
	requests.With(protocol.Value("grpc")).Record(2)
}
