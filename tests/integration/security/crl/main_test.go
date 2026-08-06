//go:build integ

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

package crl

import (
	"context"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/security/crl/util"
)

var (
	certBundle *util.RootBundle
	clientNS   namespace.Instance
	serverNS   namespace.Instance
	client     echo.Instance
	server     echo.Instance
	configs    []echo.Config
	apps       deployment.TwoNamespaceView
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		Setup(func(ctx resource.Context) error {
			var err error
			certBundle, err = util.GenerateCaCerts(ctx)
			if err != nil {
				return err
			}
			// register cleanup of k8s resources (derived from GenerateCaCerts) that istiod distributes to every ns so we don't pollute other test suites
			ctx.Cleanup(func() {
				log.Info("cleaning up istio-ca-crl and istio-ca-root-cert ConfigMaps")
				for _, cl := range ctx.Clusters() {
					nsList, err := cl.Kube().CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
					if err != nil {
						return
					}
					for _, ns := range nsList.Items {
						_ = cl.Kube().CoreV1().ConfigMaps(ns.Name).Delete(context.TODO(), "istio-ca-crl", metav1.DeleteOptions{})
						_ = cl.Kube().CoreV1().ConfigMaps(ns.Name).Delete(context.TODO(), "istio-ca-root-cert", metav1.DeleteOptions{})
					}
				}
			})
			return nil
		}).
		Setup(istio.Setup(nil, nil, nil)).
		SetupParallel(
			namespace.Setup(&clientNS, namespace.Config{Prefix: "client", Inject: true}),
			namespace.Setup(&serverNS, namespace.Config{Prefix: "server", Inject: true}),
		).
		Setup(func(ctx resource.Context) error {
			return setupAppsConfig(ctx, &configs)
		}).
		SetupParallel(deployment.SetupTwoNamespaces(&apps, deployment.Config{
			Namespaces: []namespace.Getter{
				namespace.Future(&clientNS),
				namespace.Future(&serverNS),
			},
			Configs:             echo.ConfigFuture(&configs),
			NoExternalNamespace: true,
		})).
		Setup(func(ctx resource.Context) error {
			for _, echoInstance := range apps.All.Instances() {
				switch echoInstance.Config().Service {
				case "client":
					client = echoInstance
				case "server":
					server = echoInstance
				}
			}
			if client == nil || server == nil {
				return fmt.Errorf("failed to find client or server echo instance")
			}
			return nil
		}).
		Run()
}

func setupAppsConfig(_ resource.Context, out *[]echo.Config) error {
	*out = []echo.Config{
		{
			Service:   "client",
			Namespace: clientNS,
			Ports: []echo.Port{
				{
					Name:         "https",
					Protocol:     protocol.HTTPS,
					TLS:          true,
					WorkloadPort: 8443,
				},
			},
		},
		{
			Service:   "server",
			Namespace: serverNS,
			Ports: []echo.Port{
				{
					Name:         "https",
					Protocol:     protocol.HTTPS,
					TLS:          true,
					WorkloadPort: 8443,
				},
			},
		},
	}
	return nil
}
