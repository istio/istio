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

package untaint

import (
	"context"
	"encoding/json"
	"testing"

	iopv1alpha1 "istio.io/istio/operator/pkg/apis"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/tests/integration/security/util/cert"
)

const (
	Captured = "captured"
)

var i istio.Instance

func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		RequireMinVersion(24).
		Label(label.IPv4). // https://github.com/istio/istio/issues/41008
		Setup(func(t resource.Context) error {
			t.Settings().Ambient = true
			return nil
		}).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			// can't deploy VMs without eastwest gateway
			ctx.Settings().SkipVMs()
			cfg.DeployEastWestGW = false
			if ctx.Settings().AmbientMultiNetwork {
				cfg.SkipDeployCrossClusterSecrets = true
			}

			values := map[string]interface{}{
				"pilot": map[string]interface{}{
					"taint": map[string]interface{}{
						"enabled":   true,
						"namespace": cfg.SystemNamespace,
					},
					"env": map[string]interface{}{
						"PILOT_ENABLE_NODE_UNTAINT_CONTROLLERS": "true",
					},
				},
				"ztunnel": map[string]interface{}{
					"terminationGracePeriodSeconds": 5,
					"env": map[string]interface{}{
						"SECRET_TTL": "5m",
					},
				},
				"gateways": map[string]interface{}{
					"istio-ingressgateway": map[string]interface{}{
						"enabled": false,
					},
					"istio-egressgateway": map[string]interface{}{
						"enabled": false,
					},
				},
			}

			valuesConfigJSON, err := json.Marshal(values)
			if err != nil {
				scopes.Framework.Fatalf("failed to marshal values: %v", err)
			}
			cfg.ControlPlaneSpec = &iopv1alpha1.IstioOperatorSpec{
				Values: valuesConfigJSON,
			}
		}, cert.CreateCASecretAlt)).
		Teardown(untaintNodes).
		Run()
}

func taintNodes(t resource.Context) error {
	nodeC := t.Clusters().Default().Kube().CoreV1().Nodes()
	nodes, err := nodeC.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

Outer:
	for _, node := range nodes.Items {
		for _, taint := range node.Spec.Taints {
			if taint.Key == "cni.istio.io/not-ready" {
				continue Outer
			}
		}
		node.Spec.Taints = append(node.Spec.Taints, corev1.Taint{
			Key:    "cni.istio.io/not-ready",
			Value:  "true",
			Effect: corev1.TaintEffectNoSchedule,
		})
		_, err := nodeC.Update(context.TODO(), &node, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}

// Untaint nodes if the test failed, so we restore the cluster to a usable state.
func untaintNodes(t resource.Context) {
	nodeC := t.Clusters().Default().
		Kube().CoreV1().Nodes()
	nodes, err := nodeC.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		// TODO: log
		return
	}

	for _, node := range nodes.Items {
		var taints []corev1.Taint
		for _, taint := range node.Spec.Taints {
			if taint.Key == "cni.istio.io/not-ready" {
				continue
			}
			taints = append(taints, taint)
		}
		if len(taints) != len(node.Spec.Taints) {
			node.Spec.Taints = taints
			_, err := nodeC.Update(context.TODO(), &node, metav1.UpdateOptions{})
			if err != nil {
				panic(err)
			}
		}
	}
}
