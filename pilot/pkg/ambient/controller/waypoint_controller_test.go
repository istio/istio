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

package controller

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/gateway-api/apis/v1alpha2"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/file"
)

func TestRenderImagePullPolicy(t *testing.T) {
	policy := corev1.PullIfNotPresent
	vc, err := inject.NewValuesConfig(fmt.Sprintf(`
global:
  hub: test
  imagePullPolicy: %s
  tag: test`, policy))
	if err != nil {
		t.Fatal(err)
	}
	tmplPath := filepath.Join(env.IstioSrc, "manifests/charts/istio-control/istio-discovery/files/waypoint.yaml")
	tmplStr := file.AsStringOrFail(t, tmplPath)
	tmpl, err := inject.ParseTemplates(map[string]string{"waypoint": tmplStr})
	if err != nil {
		t.Fatal(err)
	}
	c := kube.NewFakeClient(
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "sa-1", Namespace: "test"}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "sa-2", Namespace: "test"}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "sa-3", Namespace: "cross-namespace"}},
	)
	cc := NewWaypointProxyController(c, "test", func() inject.WebhookConfig {
		return inject.WebhookConfig{
			Templates: tmpl,
			Values:    vc,
		}
	}, func(func()) {})
	input := MergedInput{
		Namespace:      "default",
		GatewayName:    "gateway",
		UID:            "uuid",
		ServiceAccount: "sa",
		Cluster:        "cluster1",
	}
	deploy, err := cc.RenderDeploymentApply(input)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range deploy.Spec.Template.Spec.Containers {
		if *c.ImagePullPolicy != policy {
			t.Fatal(err)
		}
	}
}

func TestWaypointProxyController(t *testing.T) {
	vc, err := inject.NewValuesConfig(`
global:
  hub: test
  tag: test`)
	if err != nil {
		t.Fatal(err)
	}
	tmplPath := filepath.Join(env.IstioSrc, "manifests/charts/istio-control/istio-discovery/files/waypoint.yaml")
	tmplStr := file.AsStringOrFail(t, tmplPath)
	tmpl, err := inject.ParseTemplates(map[string]string{"waypoint": tmplStr})
	if err != nil {
		t.Fatal(err)
	}
	run := func(name string, f func(t *testing.T, cc *WaypointProxyController)) {
		t.Run(name, func(t *testing.T) {
			c := kube.NewFakeClient(
				&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "sa-1", Namespace: "test"}},
				&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "sa-2", Namespace: "test"}},
				&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "sa-3", Namespace: "cross-namespace"}},
			)

			cc := NewWaypointProxyController(c, "test", func() inject.WebhookConfig {
				return inject.WebhookConfig{
					Templates: tmpl,
					Values:    vc,
				}
			}, func(func()) {})
			stop := make(chan struct{})
			t.Cleanup(func() {
				close(stop)
				cc.queue.Run(stop)
			})
			c.RunAndWait(stop)
			f(t, cc)
		})
	}

	run("empty", func(t *testing.T, cc *WaypointProxyController) {
		assert.NoError(t, cc.Reconcile(types.NamespacedName{Name: "gateway", Namespace: "test"}))
	})

	run("full namespace gateway", func(t *testing.T, cc *WaypointProxyController) {
		g := gomega.NewGomegaWithT(t)
		createGatewaysAndWait(g, cc, "test", makeGateway("gateway", ""))
		// TODO: status update will fail and log due to different fake clients being used
		assert.NoError(t, cc.Reconcile(types.NamespacedName{Name: "gateway", Namespace: "test"}))
		gws, _ := cc.client.Kube().AppsV1().Deployments("test").List(context.Background(), metav1.ListOptions{})
		g.Expect(gws.Items).To(
			gomega.And(
				gomega.ConsistOf(Names("gateway")),
				gomega.HaveEach(HaveOwner("gateway", "Gateway")),
			),
		)
	})

	run("single SA gateway", func(t *testing.T, cc *WaypointProxyController) {
		g := gomega.NewGomegaWithT(t)
		createGatewaysAndWait(g, cc, "test", makeGateway("gateway", "sa-1"))
		// TODO: status update will fail and log due to different fake clients being used
		assert.NoError(t, cc.Reconcile(types.NamespacedName{Name: "gateway", Namespace: "test"}))
		gws, _ := cc.client.Kube().AppsV1().Deployments("test").List(context.Background(), metav1.ListOptions{})
		g.Expect(gws.Items).To(
			gomega.And(
				gomega.ConsistOf(Names("gateway")),
				gomega.HaveEach(HaveOwner("gateway", "Gateway")),
			),
		)
	})
}

func makeGateway(s string, sa string) *v1alpha2.Gateway {
	gw := &v1alpha2.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s,
			Namespace: "test",
		},
		Spec: v1alpha2.GatewaySpec{
			GatewayClassName: "istio-mesh",
		},
	}
	if sa != "" {
		gw.Annotations = map[string]string{
			"istio.io/service-account": sa,
		}
	}
	return gw
}

func HaveName(name string) gomega.OmegaMatcher {
	return gomega.HaveField("ObjectMeta", gomega.HaveField("Name", name))
}

func Names(names ...string) []gomega.OmegaMatcher {
	elements := []gomega.OmegaMatcher{}
	for _, name := range names {
		elements = append(elements, HaveName(name))
	}
	return elements
}

func HaveOwner(name, kind string) gomega.OmegaMatcher {
	return gomega.HaveField("ObjectMeta",
		gomega.HaveField("OwnerReferences",
			gomega.ContainElement(
				gomega.And(gomega.HaveField("Name", name),
					gomega.HaveField("Kind", kind)))))
}

func createGatewaysAndWait(g gomega.Gomega, cc *WaypointProxyController, ns string, gws ...*v1alpha2.Gateway) {
	for _, gw := range gws {
		g.Expect(cc.client.GatewayAPI().GatewayV1alpha2().Gateways(ns).
			Create(context.Background(), gw, metav1.CreateOptions{})).
			Error().NotTo(gomega.HaveOccurred())
	}
	g.Eventually(func() ([]*v1alpha2.Gateway, error) {
		return cc.gateways.Gateways(ns).List(labels.Everything())
	}).Should(gomega.ConsistOf(gws))
}
