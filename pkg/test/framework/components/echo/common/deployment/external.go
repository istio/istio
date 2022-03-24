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

package deployment

import (
	"strconv"

	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

const (
	ExternalSvc      = "external"
	externalHostname = "fake.external.com"
)

type External struct {
	// Namespace where external echo app will be deployed
	Namespace namespace.Instance

	// All external echo instances with no sidecar injected
	All echo.Instances
}

func (e External) build(b deployment.Builder) deployment.Builder {
	return b.WithConfig(echo.Config{
		Service:           ExternalSvc,
		Namespace:         e.Namespace,
		DefaultHostHeader: externalHostname,
		Ports:             ports.All(),
		Subsets: []echo.SubsetConfig{
			{
				Annotations: map[echo.Annotation]*echo.AnnotationValue{
					echo.SidecarInject: {
						Value: strconv.FormatBool(false),
					},
				},
			},
		},
	})
}

func (e *External) loadValues(echos echo.Instances) error {
	e.All = match.ServiceName(echo.NamespacedName{Name: ExternalSvc, Namespace: e.Namespace}).GetMatches(echos)
	return nil
}
