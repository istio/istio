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

package upgrade

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"
	kubeApiCore "k8s.io/api/core/v1"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/protocol"
	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/framework/resource"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
	helmtest "istio.io/istio/tests/integration/helm"
	"istio.io/istio/tests/integration/operator/common"
	"istio.io/istio/tests/util"
)

const (
	fromVersion          = "1.8.2"
	aSvc                 = "a" // Used by the custom gateway
	bSvc                 = "b" // Used to verify access via the custom gateway
	customServiceGateway = "custom-ingressgateway"
	iopCPTemplate        = `
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  namespace: ` + common.IstioNamespace + `
  name: control-plane
spec:
  profile: default
`
	iopCGWTemplate = `
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  name: custom-ingressgateway-iop
  namespace: ` + common.IstioNamespace + `
spec:
  profile: empty
  hub: %s
  tag: %s # NOTE version
  components:
    ingressGateways:
      - name: ` + customServiceGateway + `
        label:
          istio: ` + customServiceGateway + `
        namespace: %s
        enabled: true
`
	gwTemplate = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: %s
spec:
  selector:
    istio: %s
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
`
	vsTemplate = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: %s
spec:
  hosts:
  - "*"
  gateways:
  - %s
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: %s
`
)

type echoDeployments struct {
	appANamespace, appBNamespace namespace.Instance
	A, B                         echo.Instances
}

var (
	apps              = &echoDeployments{}
	customGWNamespace namespace.Instance
)

// TestUpdateWithCustomGateway tests access to an application using an updated
// control plane and custom gateway
func TestUpdateWithCustomGateway(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ingress.gateway").
		Run(func(ctx framework.TestContext) {
			var err error
			cs := ctx.Clusters().Default().(*kubecluster.Cluster)

			// Create Istio operator of the specified version
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{Cluster: ctx.Environment().Clusters()[0]})
			initOperatorCmd := []string{
				"operator", "init", "--manifests", common.ManifestPath,
				"--hub", "gcr.io/istio-release", "--tag", fromVersion,
			}
			_, _ = istioCtl.InvokeOrFail(t, initOperatorCmd)

			// configs hold the yaml files that need to be deleted when the test is over
			// Also clean up the istio control plane
			configs := make(map[string]string)
			ctx.ConditionalCleanup(func() {
				for _, config := range configs {
					ctx.Config().DeleteYAML("istio-system", config)
				}

				// Wait until any deployments and services in the control plane and
				// custom gateway namespaces are gone
				retry.UntilSuccessOrFail(t, func() error {
					for _, n := range []string{common.IstioNamespace, customGWNamespace.Name()} {
						if svc, _ := cs.CoreV1().Services(n).Get(context.TODO(), n, kubeApiMeta.GetOptions{}); svc.Name != "" {
							return fmt.Errorf("got service: %s in namespace: %s, expected to be removed", svc.Name, n)
						}
						if dp, _ := cs.AppsV1().Deployments(n).Get(context.TODO(), n, kubeApiMeta.GetOptions{}); dp.Name != "" {
							return fmt.Errorf("got deployment %s in namespace: %s expected to be removed", dp.Name, n)
						}
					}
					return nil
				}, retry.Timeout(common.RetryTimeOut), retry.Delay(common.RetryDelay))

				// now remove operator
				removeCmd := []string{
					"operator", "remove",
				}
				istioCtl.InvokeOrFail(t, removeCmd)

				// wait until operator pods are gone
				retry.UntilSuccessOrFail(t, func() error {
					if pods, _ := cs.PodsForSelector(context.TODO(), common.OperatorNamespace); len(pods.Items) != 0 {
						return fmt.Errorf("pods from cluster, expected to be removed: %+v", pods)
					}
					return nil
				}, retry.Timeout(common.RetryTimeOut), retry.Delay(common.RetryDelay))
			})

			// Create the istio-system namespace and Istio control plane of the specified version
			helmtest.CreateIstioSystemNamespace(t, cs)
			configs[iopCPTemplate] = iopCPTemplate
			if err := ctx.Config().ApplyYAMLNoCleanup("istio-system", iopCPTemplate); err != nil {
				ctx.Fatal(err)
			}
			WaitForCPInstallation(ctx, cs)

			// Create Custom gateway of the specified version
			// Create namespace for custom gateway
			if customGWNamespace, err = namespace.New(ctx, namespace.Config{
				Prefix: "custom-gw",
				Inject: false,
			}); err != nil {
				t.Fatalf("failed to create custom gateway namespace: %v", err)
			}

			// Install custom gateway of the specified version
			gatewayConfig := fmt.Sprintf(iopCGWTemplate, "gcr.io/istio-release", fromVersion, customGWNamespace.Name())
			configs[gatewayConfig] = gatewayConfig
			if err := ctx.Config().ApplyYAMLNoCleanup("istio-system", gatewayConfig); err != nil {
				ctx.Fatal(err)
			}
			WaitForCGWInstallation(ctx, cs)

			// Install the apps
			if err = setupApps(ctx, apps); err != nil {
				ctx.Fatal(err)
			}

			// Apply a gateway to the custom-gateway and a virtual service for appplication A in its namespace.
			// Application A will then be exposed externally on the custom-gateway
			gwYaml := fmt.Sprintf(gwTemplate, aSvc+"-gateway", customServiceGateway)
			ctx.Config().ApplyYAMLOrFail(ctx, apps.appANamespace.Name(), gwYaml)
			vsYaml := fmt.Sprintf(vsTemplate, aSvc, aSvc+"-gateway", aSvc)
			ctx.Config().ApplyYAMLOrFail(ctx, apps.appANamespace.Name(), vsYaml)

			// Verify that one can access application A on the custom-gateway
			gwIngressURL, err := getIngressURL(customGWNamespace.Name(), customServiceGateway)
			if err != nil {
				t.Fatalf("failed to get custom gateway URL: %v", err)
			}
			gwAddress := (strings.Split(gwIngressURL, ":"))[0]

			ctx.NewSubTest("gateway and service applied").Run(func(ctx framework.TestContext) {
				apps.B[0].CallWithRetryOrFail(t, echo.CallOptions{
					Target:    apps.A[0],
					PortName:  "http",
					Address:   gwAddress,
					Path:      "/",
					Validator: echo.ExpectOK(),
				}, retry.Timeout(time.Minute))
			})

			// Upgrade to control plane
			s, err := image.SettingsFromCommandLine()
			if err != nil {
				t.Fatalf("failed to get settings: %v", err)
			}
			istioCtl.InvokeOrFail(t, []string{
				"operator", "init", "--manifests", common.ManifestPath, "--hub", s.Hub, "--tag", s.Tag,
			})

			// Wait for control plane upgrade
			WaitForCPUpgrade(ctx, cs, s.Hub, s.Tag)

			// Verify that one can access application A on the custom-gateway
			ctx.NewSubTest("gateway and service applied, upgraded control plane").Run(func(ctx framework.TestContext) {
				apps.B[0].CallWithRetryOrFail(t, echo.CallOptions{
					Target:    apps.A[0],
					PortName:  "http",
					Address:   gwAddress,
					Path:      "/",
					Validator: echo.ExpectOK(),
				}, retry.Timeout(time.Minute))
			})

			// Upgrade the custom gateway
			upgradedGatewayConfig := fmt.Sprintf(iopCGWTemplate, s.Hub, s.Tag, customGWNamespace.Name())
			configs[upgradedGatewayConfig] = upgradedGatewayConfig
			if err := ctx.Config().ApplyYAMLNoCleanup("istio-system", upgradedGatewayConfig); err != nil {
				ctx.Fatal(err)
			}

			// Wait for custom gateway upgrade
			WaitForCGWUpgrade(ctx, cs, s.Hub, s.Tag)

			ctx.NewSubTest("gateway and service applied, upgraded control plane and custom gateway").Run(func(ctx framework.TestContext) {
				apps.B[0].CallWithRetryOrFail(t, echo.CallOptions{
					Target:    apps.A[0],
					PortName:  "http",
					Address:   gwAddress,
					Path:      "/",
					Validator: echo.ExpectOK(),
				}, retry.Timeout(time.Minute))
			})
		})
}

/// WaitForCPInstallation waits until the control plane installation is complete
func WaitForCPInstallation(ctx framework.TestContext, cs cluster.Cluster) {
	scopes.Framework.Infof("=== waiting on istio control installation === ")

	retry.UntilSuccessOrFail(ctx, func() error {
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, common.IstioNamespace, "app=istiod")); err != nil {
			return fmt.Errorf("istiod pod is not ready: %v", err)
		}
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, common.IstioNamespace, "app=istio-ingressgateway")); err != nil {
			return fmt.Errorf("istio ingress gateway pod is not ready: %v", err)
		}
		return nil
	}, retry.Timeout(common.RetryTimeOut), retry.Delay(common.RetryDelay))

	// At this point, creating namespaces and apps via SetupApps will typically see the apps
	// not having Istio injected, so also wait on the mutating webhook.
	retry.UntilSuccessOrFail(ctx, func() error {
		if _, err := cs.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.TODO(), "istio-sidecar-injector",
			kubeApiMeta.GetOptions{}); err != nil {
			return fmt.Errorf("mutating webhook is not ready: %v", err)
		}
		return nil
	}, retry.Timeout(common.RetryTimeOut), retry.Delay(common.RetryDelay))
	scopes.Framework.Infof("=== succeeded ===")
}

/// WaitForCPUpgrade waits until the control plane upgrade is complete using images with hub and tag.
func WaitForCPUpgrade(ctx framework.TestContext, cs cluster.Cluster, hub, tag string) {
	scopes.Framework.Infof("=== waiting on istio control upgrade === ")

	retry.UntilSuccessOrFail(ctx, func() error {
		if _, err := CheckPodsWithImageAreReady(kubetest.NewPodFetch(cs, common.IstioNamespace, "app=istiod"),
			hub+"/pilot:"+tag); err != nil {
			return fmt.Errorf("istiod pod is not ready: %v", err)
		}
		if _, err := CheckPodsWithImageAreReady(kubetest.NewPodFetch(cs, common.IstioNamespace, "app=istio-ingressgateway"),
			hub+"/proxyv2:"+tag); err != nil {
			return fmt.Errorf("istio ingress gateway pod is not ready: %v", err)
		}
		return nil
	}, retry.Timeout(common.RetryTimeOut), retry.Delay(common.RetryDelay))
}

// WaitForCGWInstallation waits until the custom gateway installation is complete
func WaitForCGWInstallation(ctx framework.TestContext, cs cluster.Cluster) {
	scopes.Framework.Infof("=== waiting on custom gateway installation === ")

	retry.UntilSuccessOrFail(ctx, func() error {
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, customGWNamespace.Name(), "app=istio-ingressgateway")); err != nil {
			return fmt.Errorf("custom gateway pod is not ready: %v", err)
		}
		return nil
	}, retry.Timeout(common.RetryTimeOut), retry.Delay(common.RetryDelay))
	scopes.Framework.Infof("=== succeeded ===")
}

/// WaitForCGWUpgrade waits until the custom gateway upgrade is complete using images with hub and tag.
func WaitForCGWUpgrade(ctx framework.TestContext, cs cluster.Cluster, hub, tag string) {
	scopes.Framework.Infof("=== waiting on istio control upgrade === ")

	retry.UntilSuccessOrFail(ctx, func() error {
		if _, err := CheckPodsWithImageAreReady(kubetest.NewPodFetch(cs, customGWNamespace.Name(), "app=istio-ingressgateway"),
			hub+"/proxyv2:"+tag); err != nil {
			return fmt.Errorf("istio ingress gateway pod is not ready: %v", err)
		}
		return nil
	}, retry.Timeout(common.RetryTimeOut), retry.Delay(common.RetryDelay))
}

// setupApps creates two namespaces and starts an echo app in each namespace.
// Tests will be able to connect the apps to gateways and verify traffic.
func setupApps(ctx resource.Context, apps *echoDeployments) error {
	var err error
	var echos echo.Instances

	// Setup namespace for app a
	if apps.appANamespace, err = namespace.New(ctx, namespace.Config{
		Prefix: "app-a",
		Inject: true,
	}); err != nil {
		return err
	}

	// Setup namespace for app b
	if apps.appBNamespace, err = namespace.New(ctx, namespace.Config{
		Prefix: "app-b",
		Inject: true,
	}); err != nil {
		return err
	}

	// Setup the two apps, one per namespace
	builder := echoboot.NewBuilder(ctx)
	builder.
		WithClusters(ctx.Clusters()...).
		WithConfig(echoConfig(aSvc, apps.appANamespace)).
		WithConfig(echoConfig(bSvc, apps.appBNamespace))

	if echos, err = builder.Build(); err != nil {
		return err
	}
	apps.A = echos.Match(echo.Service(aSvc))
	apps.B = echos.Match(echo.Service(bSvc))
	return nil
}

func echoConfig(name string, ns namespace.Instance) echo.Config {
	return echo.Config{
		Service:   name,
		Namespace: ns,
		Ports: []echo.Port{
			{
				Name:     "http",
				Protocol: protocol.HTTP,
				// We use a port > 1024 to not require root
				InstancePort: 8090,
			},
		},
		Subsets: []echo.SubsetConfig{{}},
	}
}

func getIngressURL(ns, service string) (string, error) {
	retry := util.Retrier{
		BaseDelay: 10 * time.Second,
		Retries:   3,
		MaxDelay:  30 * time.Second,
	}
	var url string

	retryFn := func(_ context.Context, i int) error {
		hostCmd := fmt.Sprintf(
			"kubectl get service %s -n %s -o jsonpath='{.status.loadBalancer.ingress[0].ip}'",
			service, ns)
		portCmd := fmt.Sprintf(
			"kubectl get service %s -n %s -o jsonpath='{.spec.ports[?(@.name==\"http2\")].port}'",
			service, ns)
		host, err := shell.Execute(false, hostCmd)
		if err != nil {
			return fmt.Errorf("error executing the cmd (%v): %v", hostCmd, err)
		}
		port, err := shell.Execute(false, portCmd)
		if err != nil {
			return fmt.Errorf("error executing the cmd (%v): %v", portCmd, err)
		}
		url = strings.Trim(host, "'") + ":" + strings.Trim(port, "'")
		return nil
	}

	if _, err := retry.Retry(context.Background(), retryFn); err != nil {
		return url, fmt.Errorf("getIngressURL retry failed with err: %v", err)
	}
	return url, nil
}

// CheckPodsWithImageAreReady checks whether the pods that are selected by the given function is in ready state or not.
func CheckPodsWithImageAreReady(fetchFunc kubetest.PodFetchFunc, image string) ([]kubeApiCore.Pod, error) {
	scopes.Framework.Infof("Checking pods with image %s ready...", image)

	fetched, err := fetchFunc()
	if err != nil {
		scopes.Framework.Infof("Failed retrieving pods: %v", err)
		return nil, err
	}

	for i, p := range fetched {
		for _, container := range p.Spec.Containers {
			if container.Image == image {
				msg := "Ready"
				if e := istioKube.CheckPodReady(&p); e != nil {
					msg = e.Error()
					err = multierror.Append(err, fmt.Errorf("%s/%s: %s", p.Namespace, p.Name, msg))
				}
				scopes.Framework.Infof("  [%2d] %45s %15s (%v)", i, p.Name, p.Status.Phase, msg)
				break
			}
			err = fmt.Errorf("pod with image %s not found", image)
		}
	}

	if err != nil {
		return nil, err
	}

	return fetched, nil
}
