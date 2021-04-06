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

package monitoring

import "istio.io/pkg/monitoring"

// RequestType specifies the type of request we are monitoring. Current supported are CSR and TokenExchange
var RequestType = monitoring.MustCreateLabel("request_type")

const (
	TokenExchange = "token_exchange"
	CSR           = "csr"
)

var NumOutgoingRetries = monitoring.NewSum(
	"num_outgoing_retries",
	"Number of outgoing retry requests (e.g. to a token exchange server, CA, etc.)",
	monitoring.WithLabels(RequestType))

func init() {
	monitoring.MustRegister(
		NumOutgoingRetries,
	)
}

func Reset() {
	NumOutgoingRetries.Record(0)
}
