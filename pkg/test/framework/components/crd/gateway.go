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

package crd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/util/retry"
)

// SupportsGatewayAPI checks if the gateway API is supported.
func SupportsGatewayAPI(t resource.Context) bool {
	for _, cluster := range t.Clusters() {
		if !cluster.MinKubeVersion(23) { // API uses CEL which requires 1.23
			return false
		}
	}
	return true
}

// SupportsGatewayAPIInferenceExtension checks if the gateway API inference extension is supported.
func SupportsGatewayAPIInferenceExtension(t resource.Context) bool {
	return SupportsGatewayAPI(t)
}

func DeployGatewayAPIOrSkip(ctx framework.TestContext) {
	res := DeployGatewayAPI(ctx)
	if res == errSkip {
		ctx.Skip(errSkip.Error())
	}
	if res != nil {
		ctx.Fatal(res)
	}
}

func DeployGatewayAPIInferenceExtensionOrSkip(ctx framework.TestContext) {
	res := DeployGatewayAPIInferenceExtension(ctx)
	if res == errSkip {
		ctx.Skip(errSkip.Error())
	}
	if res != nil {
		ctx.Fatal(res)
	}
}

func DeployGatewayAPI(ctx resource.Context) error {
	cfg, _ := istio.DefaultConfig(ctx)
	if !cfg.DeployGatewayAPI {
		return nil
	}
	if !SupportsGatewayAPI(ctx) {
		return errSkip
	}

	// Deploy and wait for Gateway API CRDs
	crdFile := filepath.Join(env.IstioSrc, "tests/integration/pilot/testdata/gateway-api-crd.yaml")
	if err := deployAndWaitForCRDs(ctx, crdFile, "gateway.networking.k8s.io"); err != nil {
		return err
	}

	// Wait until our GatewayClass is ready
	return retry.UntilSuccess(func() error {
		for _, c := range ctx.Clusters().Configs() {
			_, err := c.GatewayAPI().GatewayV1beta1().GatewayClasses().Get(context.Background(), "istio", metav1.GetOptions{})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func DeployGatewayAPIInferenceExtension(ctx resource.Context) error {
	cfg, _ := istio.DefaultConfig(ctx)
	if !cfg.DeployGatewayAPI {
		return nil
	}
	if !SupportsGatewayAPIInferenceExtension(ctx) {
		return errSkip
	}

	// Deploy and wait for inference extension CRDs
	crdFile := filepath.Join(env.IstioSrc, "tests/integration/pilot/testdata/gateway-api-inference-extension-crd.yaml")

	// Check for both inference.networking.k8s.io and inference.networking.x-k8s.io CRDs
	if err := deployAndWaitForCRDs(ctx, crdFile, "inference.networking.k8s.io"); err != nil {
		return err
	}
	return deployAndWaitForCRDs(ctx, crdFile, "inference.networking.x-k8s.io")
}

var errSkip = errors.New("not supported; requires CRDv1 support")

// deployAndWaitForCRDs applies a CRD file and waits for the CRDs to become ready.
// It checks for CRDs ending with the specified suffix and validates their Established condition.
func deployAndWaitForCRDs(ctx resource.Context, crdFile string, crdSuffix string) error {
	if err := ctx.ConfigIstio().
		File("", crdFile).
		Apply(apply.NoCleanup); err != nil {
		return err
	}

	// Wait until CRDs are ready
	return retry.UntilSuccess(func() error {
		for _, c := range ctx.Clusters().Configs() {
			crdl, err := c.Ext().ApiextensionsV1().CustomResourceDefinitions().List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}
			for _, crd := range crdl.Items {
				if !strings.HasSuffix(crd.Name, crdSuffix) {
					continue
				}
				found := false
				for _, condition := range crd.Status.Conditions {
					if condition.Type == apiextensions.Established && condition.Status == apiextensions.ConditionTrue {
						found = true
					}
				}
				if !found {
					return fmt.Errorf("crd %v not ready: %+v", crd.Name, crd.Status)
				}
			}
		}
		return nil
	})
}
