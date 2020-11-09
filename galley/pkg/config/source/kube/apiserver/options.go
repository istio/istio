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

package apiserver

import (
	"time"

	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver/status"
	"istio.io/istio/pkg/config/schema/collection"
)

// Options for the kube controller
type Options struct {
	// The Client interfaces to use for connecting to the API server.
	Client kube.Interfaces

	ResyncPeriod time.Duration

	Schemas collection.Schemas

	StatusController status.Controller

	WatchedNamespaces string
}
