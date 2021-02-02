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

package gatewayupgrade

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/env"
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
	"istio.io/istio/tests/util"
)

const (
	fromVersion          = "1.8.2"
	aSvc                 = "a" // Used by the default gateway
	bSvc                 = "b" // Used by the custom ingress gateway
	IstioNamespace       = "istio-system"
	retryDelay           = 2 * time.Second
	RetryTimeOut         = 5 * time.Minute
	customServiceGateway = "custom-ingressgateway"
	vsTemplate           = `
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
)

type echoDeployments struct {
	appANamespace, appBNamespace namespace.Instance
	A, B                         echo.Instances
}

var (
	apps              = &echoDeployments{}
	customGWNamespace namespace.Instance
	// ManifestPath is path of local manifests which istioctl operator init refers to.
	ManifestPath = filepath.Join(env.IstioSrc, "manifests")
)

// TestUpdateWithCustomGateway tests access to an aplication using an updated
// control plane and custom gateway
func TestUpdateWithCustomGateway(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ingress.gateway").
		Run(func(ctx framework.TestContext) {

			var err error
			cs := ctx.Clusters().Default().(*kubecluster.Cluster)

			// Create Istio control plane of the specified version
			// configs contains items to clean up in the istio-system namespace
			configs := make(map[string]string)
			ctx.ConditionalCleanup(func() {
				for _, config := range configs {
					ctx.Config().DeleteYAML("istio-system", config)
				}
			})

			// Install control plane of the specified version
			helmtest.CreateIstioSystemNamespace(t, cs)
			installCPOrFail(ctx, fromVersion, "istio-system", configs)
			WaitForCPInstallation(ctx, cs)

			// Create Custom gateway of the specified version
			// Create namespace for custom gateway
			if customGWNamespace, err = namespace.New(ctx, namespace.Config{
				Prefix: "custom-gw",
				Inject: false,
			}); err != nil {
				t.Fatalf("failed to create custom gateway namespace: %v", err)
			}

			// gwConfigs contains items to clean up in the custom cgateway namespace
			gwConfigs := make(map[string]string)
			ctx.ConditionalCleanup(func() {
				for _, config := range gwConfigs {
					ctx.Config().DeleteYAML(customGWNamespace.Name(), config)
				}
			})

			// Install custom gateway of the specified version
			installCGWOrFail(ctx, fromVersion, customGWNamespace.Name(), gwConfigs)
			WaitForCGWInstallation(ctx, cs)

			// Install the apps
			setupApps(ctx, apps)

			// Verify apps can access each other. TODO

			// Apply a gateway to the custom-gateway and a virtual service for appplication A in its namespace.
			// Application A will then be exposed externally on the custom-gateway
			gwYaml := fmt.Sprintf(gwTemplate, aSvc+"-gateway", customServiceGateway)
			ctx.Config().ApplyYAMLOrFail(ctx, apps.appANamespace.Name(), gwYaml)
			vsYaml := fmt.Sprintf(vsTemplate, aSvc, aSvc+"-gateway", aSvc)
			ctx.Config().ApplyYAMLOrFail(ctx, apps.appANamespace.Name(), vsYaml)

			// Verify that one can access application A on the custom-gateway
			// Unable to find the ingress for the custom gateway via the framework so retrieve URL and
			// use in the echo call. TODO - fix to use framework - may need framework updates
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
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{Cluster: ctx.Environment().Clusters()[0]})
			istioCtl.InvokeOrFail(t, []string{"upgrade", "--manifests=" + ManifestPath, "--skip-confirmation",
				"--set", "hub=" + s.Hub, "--set", "tag=" + s.Tag, "-f", "../testdata/upgrade/base.yaml"})

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
			// Create a temp file for the custom gateway with the random namespace name
			tempFile, err := ioutil.TempFile(ctx.WorkDir(), "modified_customgw.yaml")
			input, err := ioutil.ReadFile("../testdata/upgrade/customgw.yaml")
			if err != nil {
				ctx.Fatal(err)
			}
			output := bytes.Replace(input, []byte("custom-gateways"), []byte(customGWNamespace.Name()), -1)
			if _, err = tempFile.Write(output); err != nil {
				ctx.Fatal(err)
			}

			// Upgrade to custom gateway
			istioCtl.InvokeOrFail(t, []string{"upgrade", "--manifests=" + ManifestPath, "--skip-confirmation",
				"--set", "hub=" + s.Hub, "--set", "tag=" + s.Tag, "--namespace", customGWNamespace.Name(),
				"-f", tempFile.Name()})

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

// installCPOrFail takes an Istio version and installs a control plane running that version.
// The provided istio version must be present in tests/integration/pilot/testdata/upgrade for the installation to succeed.
func installCPOrFail(ctx framework.TestContext, version, namespace string, configs map[string]string) {
	config, err := ReadInstallFile(fmt.Sprintf("%s-base-install.yaml", version))
	if err != nil {
		ctx.Fatalf("could not read installation config: %v", err)
	}

	configs[version] = config
	if err := ctx.Config().ApplyYAMLNoCleanup(namespace, config); err != nil {
		ctx.Fatal(err)
	}
}

// WaitForCPInstallation waits until the control plane installation is complete
func WaitForCPInstallation(ctx framework.TestContext, cs cluster.Cluster) {
	scopes.Framework.Infof("=== waiting on istio control installation === ")

	retry.UntilSuccessOrFail(ctx, func() error {
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, IstioNamespace, "app=istiod")); err != nil {
			return fmt.Errorf("istiod pod is not ready: %v", err)
		}
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, IstioNamespace, "app=istio-ingressgateway")); err != nil {
			return fmt.Errorf("istio ingress gateway pod is not ready: %v", err)
		}
		return nil
	}, retry.Timeout(RetryTimeOut), retry.Delay(retryDelay))
	scopes.Framework.Infof("=== succeeded ===")
}

// installCGWOrFail takes an Istio version and installs a custom gateway running that version.
// The provided istio version must be present in tests/integration/pilot/testdata/upgrade for the installation to succeed.
func installCGWOrFail(ctx framework.TestContext, version, namespace string, configs map[string]string) {
	config, err := ReadInstallFile(fmt.Sprintf("%s-cgw-install.yaml", version))
	if err != nil {
		ctx.Fatalf("could not read installation config: %v", err)
	}

	// Update namespace in config
	updatedConfig := strings.ReplaceAll(config, "custom-gateways", namespace)

	configs[version] = updatedConfig
	if err := ctx.Config().ApplyYAMLNoCleanup(namespace, updatedConfig); err != nil {
		ctx.Fatal(err)
	}
}

// WaitForCGWInstallation waits until the custom gateway installation is complete
func WaitForCGWInstallation(ctx framework.TestContext, cs cluster.Cluster) {
	scopes.Framework.Infof("=== waiting on custom gateway installation === ")

	retry.UntilSuccessOrFail(ctx, func() error {
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, customGWNamespace.Name(), "app=istio-ingressgateway")); err != nil {
			return fmt.Errorf("custom gateway pod is not ready: %v", err)
		}
		return nil
	}, retry.Timeout(RetryTimeOut), retry.Delay(retryDelay))
	scopes.Framework.Infof("=== succeeded ===")
}

// ReadInstallFile reads a tar compressed installation file
func ReadInstallFile(f string) (string, error) {
	b, err := ioutil.ReadFile(filepath.Join("../testdata/upgrade", f+".tar"))
	if err != nil {
		return "", err
	}
	tr := tar.NewReader(bytes.NewBuffer(b))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return "", err
		}
		if hdr.Name != f {
			continue
		}
		contents, err := ioutil.ReadAll(tr)
		if err != nil {
			return "", err
		}
		return string(contents), nil
	}
	return "", fmt.Errorf("file not found")
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
