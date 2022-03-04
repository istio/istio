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
	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	dto "github.com/prometheus/client_model/go"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

// Sidecar provides an interface to execute queries against a single Envoy sidecar.
type Sidecar interface {
	// Info about the Envoy instance.
	Info() (*envoyAdmin.ServerInfo, error)
	InfoOrFail(t test.Failer) *envoyAdmin.ServerInfo

	// Config of the Envoy instance.
	Config() (*envoyAdmin.ConfigDump, error)
	ConfigOrFail(t test.Failer) *envoyAdmin.ConfigDump

	// WaitForConfig queries the Envoy configuration an executes the given accept handler. If the
	// response is not accepted, the request will be retried until either a timeout or a response
	// has been accepted.
	WaitForConfig(accept func(*envoyAdmin.ConfigDump) (bool, error), options ...retry.Option) error
	WaitForConfigOrFail(t test.Failer, accept func(*envoyAdmin.ConfigDump) (bool, error), options ...retry.Option)

	// Clusters for the Envoy instance
	Clusters() (*envoyAdmin.Clusters, error)
	ClustersOrFail(t test.Failer) *envoyAdmin.Clusters

	// Listeners for the Envoy instance
	Listeners() (*envoyAdmin.Listeners, error)
	ListenersOrFail(t test.Failer) *envoyAdmin.Listeners

	// Logs returns the logs for the sidecar container
	Logs() (string, error)
	// LogsOrFail returns the logs for the sidecar container, or aborts if an error is found
	LogsOrFail(t test.Failer) string
	Stats() (map[string]*dto.MetricFamily, error)
	StatsOrFail(t test.Failer) map[string]*dto.MetricFamily
}
