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

package util

import (
	"regexp"

	"istio.io/istio/pkg/config/constants"
)

const (
	DefaultKubernetesDomain   = "svc." + constants.DefaultKubernetesDomain
	ExportToNamespaceLocal    = "."
	ExportToAllNamespaces     = "*"
	IstioProxyName            = "istio-proxy"
	MeshGateway               = "mesh"
	Wildcard                  = "*"
	MeshConfigName            = "istio"
	InjectionLabelName        = "istio-injection"
	InjectionLabelEnableValue = "enabled"
)

var (
	fqdnPattern = regexp.MustCompile(`^(.+)\.(.+)\.svc\.cluster\.local$`)
)
