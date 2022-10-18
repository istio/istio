//go:build integ
// +build integ

//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package sdstlsorigination

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

var (
	// Namespaces
	echo1NS    namespace.Instance
	externalNS namespace.Instance

	// Servers
	apps deployment.SingleNamespaceView
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Setup(istio.Setup(nil, nil)).
		// Create namespaces first. This way, echo can correctly configure egress to all namespaces.
		SetupParallel(
			namespace.Setup(&echo1NS, namespace.Config{Prefix: "echo1", Inject: true}),
			namespace.Setup(&externalNS, namespace.Config{Prefix: "external", Inject: false})).
		SetupParallel(
			deployment.SetupSingleNamespace(&apps, deployment.Config{
				Namespaces: []namespace.Getter{
					namespace.Future(&echo1NS),
				},
				ExternalNamespace: namespace.Future(&externalNS),
			})).
		Run()
}
