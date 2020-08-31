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

package centralremotekubeconfig

import (
	"context"
	"testing"

	kubeApiCore "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/tests/integration/multicluster"
)

var (
	ist istio.Instance
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.Multicluster).
		RequireMinClusters(2).
		Setup(kube.Setup(func(s *kube.Settings, ctx resource.Context) {
			// Make istiod run on 2nd cluster and points to KUBECONFIG in cluster 0
			scopes.Framework.Infof("setting up istiod cluster before hand")
			s.ControlPlaneTopology = make(map[resource.ClusterIndex]resource.ClusterIndex)
			s.ConfigTopology = make(map[resource.ClusterIndex]resource.ClusterIndex)
			externalControlPlaneCluster := resource.ClusterIndex(1)
			configCluster := resource.ClusterIndex(0)
			scopes.Framework.Infof("remote cluster %s", ctx.Clusters()[configCluster].Name())
			cfg, err := istio.DefaultConfig(ctx)
			if err != nil {
				scopes.Framework.Infof("has error in creating istio cfg ")
				return
			}
			for i := 0; i < len(ctx.Clusters()); i++ {
				s.ControlPlaneTopology[resource.ClusterIndex(i)] = externalControlPlaneCluster
				s.ConfigTopology[resource.ClusterIndex(i)] = configCluster
			}
			//create related namespace ,secret and service account before hand in 2nd cluster
			istioKubeConfig, err := file.AsString(s.KubeConfig[0])
			if err != nil {
				scopes.Framework.Infof("has error in parsing kubeconfig ")
				return
			}
			istiodNS := &kubeApiCore.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: cfg.SystemNamespace,
				},
			}
			for i := 1; i < len(s.KubeConfig); i++ {
				scopes.Framework.Infof("creating resources in cluster %s", ctx.Clusters()[i].Name())
				ctx.Clusters()[i].CoreV1().Namespaces().Create(context.TODO(),
					istiodNS, metav1.CreateOptions{})

				istiokubeconfigSecret := &kubeApiCore.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name: "istio-kubeconfig",
					},
					Data: map[string][]byte{
						"config": []byte(istioKubeConfig),
					},
				}
				_, err = ctx.Clusters()[i].CoreV1().Secrets(cfg.SystemNamespace).Create(context.TODO(),
					istiokubeconfigSecret, metav1.CreateOptions{})
				if err != nil {
					scopes.Framework.Infof("has error in creating istio-kubeconfig secrets %v", err)
					return
				}
				istiodSA := &kubeApiCore.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: cfg.SystemNamespace,
						Name:      "istiod-service-account",
					},
				}
				_, err = ctx.Clusters()[i].CoreV1().ServiceAccounts(cfg.SystemNamespace).Create(context.TODO(),
					istiodSA, metav1.CreateOptions{})
				if err != nil {
					scopes.Framework.Infof("has error in creating istiod service account %v", err)
					return
				}
			}
		})).
		Setup(istio.Setup(&ist, func(cfg *istio.Config) {
			// Set the control plane values on the config.
			cfg.RemoteClusterValues =
				`components:
  base:
    enabled: true
  pilot:
    enabled: false
  telemetry:
    enabled: false
  istiodRemote:
    enabled: true
  ingressGateways:
  - enabled: false
    name: istio-ingressgateway
values:
  istiodRemote:
    injectionURL: https://istiod.istio-system.svc:15017/inject
  base:
    validationURL: https://istiod.istio-system.svc:15017/validate`
			cfg.ControlPlaneValues = `
components:
  base:
    enabled: false
  pilot:
    enabled: true
    k8s:
      service:
        type: LoadBalancer
  ingressGateways:
  - enabled: false
    name: istio-ingressgateway
values:
  global:
    operatorManageWebhooks: true
`
		})).
		Run()
}

func TestIngressGateway(t *testing.T) {
	multicluster.GatewayTest(t, "installation.multicluster.centralremotekubeconfig")
}
