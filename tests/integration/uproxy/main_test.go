//go:build integ
// +build integ

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

package uproxy

import (
	"context"
	"fmt"
	"testing"
	"time"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/ambient"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	i istio.Instance

	// Below are various preconfigured echo deployments. Whenever possible, tests should utilize these
	// to avoid excessive creation/tear down of deployments. In general, a test should only deploy echo if
	// its doing something unique to that specific test.
	apps = &EchoDeployments{}
)

type EchoDeployments struct {
	// Namespace echo apps will be deployed
	Namespace         namespace.Instance
	Remote            echo.Instances
	AltRemote         echo.Instances
	Captured          echo.Instances
	Uncaptured        echo.Instances
	SidecarRemote     echo.Instances
	SidecarCaptured   echo.Instances
	SidecarUncaptured echo.Instances
	All               echo.Instances
}

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			cfg.DeployEastWestGW = false
			cfg.ControlPlaneValues = "profile: ambient"
		})).
		Setup(func(t resource.Context) error {
			return SetupApps(t, i, apps)
		}).
		Setup(ambient.Redirection).
		Run()
}

const (
	Remote            = "remote"
	AltRemote         = "alt-remote"
	Captured          = "captured"
	Uncaptured        = "uncaptured"
	SidecarRemote     = "sidecar-remote"
	SidecarCaptured   = "sidecar-captured"
	SidecarUncaptured = "sidecar-uncaptured"
)

func SetupApps(t resource.Context, i istio.Instance, apps *EchoDeployments) error {
	var err error
	apps.Namespace, err = namespace.New(t, namespace.Config{
		Prefix: "echo",
		Inject: false,
	})
	if err != nil {
		return err
	}

	// Headless services don't work with targetPort, set to same port
	headlessPorts := make([]echo.Port, len(ports.All()))
	for i, p := range ports.All() {
		p.ServicePort = p.WorkloadPort
		headlessPorts[i] = p
	}
	builder := deployment.New(t).
		WithClusters(t.Clusters()...).
		WithConfig(echo.Config{
			Service:        Remote,
			Namespace:      apps.Namespace,
			Ports:          ports.All(),
			ServiceAccount: true,
			Subsets: []echo.SubsetConfig{{
				Replicas: 2,
				Labels: map[string]string{
					"asm-type":   "workload",
					"asm-remote": "true",
				},
			}},
		}).
		//WithConfig(echo.Config{
		//	Service:        AltRemote,
		//	Namespace:      apps.Namespace,
		//	Ports:          ports.All(),
		//	ServiceAccount: true,
		//	Subsets: []echo.SubsetConfig{{
		//		Replicas: 2,
		//		Labels: map[string]string{
		//			"asm-type":   "workload",
		//			"asm-remote": "true",
		//		},
		//	}},
		//}).
		WithConfig(echo.Config{
			Service:   Captured,
			Namespace: apps.Namespace,
			Ports:     ports.All(),
			Subsets: []echo.SubsetConfig{{
				Replicas: 2,
				Labels: map[string]string{
					"asm-type":     "workload",
					"ambient-type": "workload",
				},
			}},
		}).
		WithConfig(echo.Config{
			Service:   Uncaptured,
			Namespace: apps.Namespace,
			Ports:     ports.All(),
			Subsets: []echo.SubsetConfig{{
				Replicas: 2,
				Labels: map[string]string{
					"asm-type":     "none",
					"ambient-type": "none",
				},
			}},
		})

	if err := t.ConfigIstio().YAML(apps.Namespace.Name(), `apiVersion: gateway.networking.k8s.io/v1alpha2
kind: Gateway
metadata:
  name: remote
  annotations:
    istio.io/service-account: remote
spec:
  gatewayClassName: istio-mesh`).Apply(apply.NoCleanup); err != nil {
		return err
	}

	_, whErr := t.Clusters().Default().
		Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().
		Get(context.Background(), "istio-sidecar-injector", metav1.GetOptions{})
	if whErr != nil && !kerrors.IsNotFound(whErr) {
		return whErr
	}
	// Only setup sidecar tests if webhook is installed
	if whErr == nil {
		builder = builder.WithConfig(echo.Config{
			Service:   SidecarRemote,
			Namespace: apps.Namespace,
			Ports:     ports.All(),
			Subsets: []echo.SubsetConfig{{
				Replicas: 2,
				Labels: map[string]string{
					"asm-type":                "workload",
					"ambient-type":            "workload",
					"asm-remote":              "true",
					"sidecar.istio.io/inject": "true",
				},
			}},
		}).
			WithConfig(echo.Config{
				Service:   SidecarCaptured,
				Namespace: apps.Namespace,
				Ports:     ports.All(),
				Subsets: []echo.SubsetConfig{{
					Replicas: 2,
					Labels: map[string]string{
						"asm-type":                "workload",
						"ambient-type":            "workload",
						"sidecar.istio.io/inject": "true",
					},
				}},
			}).
			WithConfig(echo.Config{
				Service:   SidecarUncaptured,
				Namespace: apps.Namespace,
				Ports:     ports.All(),
				Subsets: []echo.SubsetConfig{{
					Replicas: 2,
					Labels: map[string]string{
						"asm-type":                "none",
						"ambient-type":            "none",
						"sidecar.istio.io/inject": "true",
					},
				}},
			})
	}

	echos, err := builder.Build()
	if err != nil {
		return err
	}
	for _, b := range echos {
		scopes.Framework.Infof("built %v", b.Config().Service)
	}
	apps.All = echos
	apps.Remote = match.ServiceName(echo.NamespacedName{Name: Remote, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Uncaptured = match.ServiceName(echo.NamespacedName{Name: Uncaptured, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Captured = match.ServiceName(echo.NamespacedName{Name: Captured, Namespace: apps.Namespace}).GetMatches(echos)
	apps.SidecarRemote = match.ServiceName(echo.NamespacedName{Name: SidecarRemote, Namespace: apps.Namespace}).GetMatches(echos)
	apps.SidecarUncaptured = match.ServiceName(echo.NamespacedName{Name: SidecarUncaptured, Namespace: apps.Namespace}).GetMatches(echos)
	apps.SidecarCaptured = match.ServiceName(echo.NamespacedName{Name: SidecarCaptured, Namespace: apps.Namespace}).GetMatches(echos)

	remoteErr := retry.UntilSuccess(func() error {
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(t.AllClusters()[0], apps.Namespace.Name(), "istio.io/gateway-name=remote")); err != nil {
			return fmt.Errorf("gateway is not ready: %v", err)
		}
		return nil
	}, retry.Timeout(time.Minute), retry.BackoffDelay(time.Millisecond*100))
	if remoteErr != nil {
		return err
	}

	return nil
}
