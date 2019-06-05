// Copyright 2019 Istio Authors
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

package galley

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/mcpserver"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/structpath"
)

var yamlCfg = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: helloworld-gateway
spec:
  selector:
    istio: ingressgateway # use istio default controller
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
`

func TestDialout_Basic(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			srv := mcpserver.NewSinkOrFail(t, ctx, mcpserver.SinkConfig{Collections: []string{"istio/networking/v1alpha3/gateways"}})

			// In case of native test, we first bring up MCP Sink server and then pass its
			// address to Galley while starting up. But this doesn't work in Kubernetes case
			// because Galley will be running without sinkAddress passed to it. So we have to
			// patch the configuration and restart Galley.
			//
			// TODO: Is it necessary to unpatch Galley?
			if err := updateGalleyDeploymentConfigWithSinkAddress(t, ctx, srv.Address()); err != nil {
				t.Fatalf("failed to patch galley deployment to add sinkAddress. %v", err)
			}
			g := galley.NewOrFail(t, ctx, galley.Config{SinkAddress: srv.Address()})

			ns := namespace.NewOrFail(t, ctx, "dialout", true)

			g.ApplyConfigOrFail(t, ns, yamlCfg)
			t.Logf("Applying configuration: %v", yamlCfg)

			retry.UntilSuccessOrFail(t, func() error {
				objects := srv.GetCollectionStateOrFail(t, "istio/networking/v1alpha3/gateways")
				if len(objects) != 1 {
					return fmt.Errorf("expected number of objects not found (current: %v)", len(objects))
				}

				structpath.ForProto(objects[0].Body).
					Equals("{.Metadata.Name}", "helloworld-gateway")

				return nil
			})
		})
}

// updateGalleyDeploymentConfigWithSinkAddress is a **HACK** to patch Galley configuration so that we can
// pass the address of the sink while running in Kubernetes environment.
func updateGalleyDeploymentConfigWithSinkAddress(t *testing.T, ctx resource.Context, address string) error {
	kubeEnv, ok := ctx.Environment().(*kube.Environment)
	if !ok {
		t.Logf("patchGalley: not kubernetes environment. So no patching needed")
		return nil
	}

	c, err := istio.DefaultConfig(ctx)
	if err != nil {
		return err
	}
	ns := c.ConfigNamespace

	// Now delete the deployment and wait until galley pod is deleted
	// TODO: DO NOT HARDCODE DEPLOYMENT NAME. Find a better way - like
	// obtaining it from Istio configuration
	galleyDeploymentName := "istio-galley"
	galleyDeploy, err := kubeEnv.GetDeployment(ns, galleyDeploymentName)
	if err != nil {
		return err
	}

	// Now patch the configuration - for galley startup command add sinkAuth=NONE
	// and --sinkAddress=<address> command-line arguments. No sanitization of passed
	// address is required as it is only for testing purposes and it is completely
	// in our control
	for i := range galleyDeploy.Spec.Template.Spec.Containers {
		c := &galleyDeploy.Spec.Template.Spec.Containers[i]
		if c.Name == "galley" {
			c.Command = append(c.Command,
				"--sinkAuthMode=NONE",
				fmt.Sprintf("--sinkAddress=%s", address))
			t.Logf("patchGalley: command: %#v", c.Command)
			break
		}
	}

	// Now deploy galley again with the patched configuration
	_, err = kubeEnv.UpdateDeployment(ns, galleyDeploy)
	if err != nil {
		return err
	}

	if err = kubeEnv.WaitUntilDeploymentIsReady(ns, galleyDeploy.Name); err != nil {
		return err
	}
	t.Logf("updateGalley: galley patched successfully with sinkAddress and sinkAuth command-line args")
	return nil
}
