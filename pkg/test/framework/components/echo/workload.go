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

package echo

import (
	"context"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/proto"
)

// Workload provides an interface for a single deployed echo server.
type Workload interface {
	// PodName gets the original pod name for the workload.
	PodName() string
	// Address returns the network address of the endpoint.
	Address() string

	// Sidecar if one was specified.
	Sidecar() Sidecar

	// ForwardEcho executes specific call from this workload.
	ForwardEcho(context.Context, *proto.ForwardEchoRequest) (echo.Responses, error)

	// Logs returns the logs for the app container
	Logs() (string, error)
	// LogsOrFail returns the logs for the app container, or aborts if an error is found
	LogsOrFail(t test.Failer) string
}
