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

package security

import (
	"testing"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/istioio"
)

// TestAuthorizationForHTTPServices simulates the task in https://www.istio.io/docs/tasks/security/authz-http/
func TestAuthorizationForHTTPServices(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/18511")
	framework.
		NewTest(t).
		Run(istioio.NewBuilder("tasks__security__authorization_for_http_services").
			Add(istioio.Script{
				Input:   istioio.Path("scripts/authz_http.txt"),
				WorkDir: env.IstioSrc,
			}, istioio.YamlResources{
				BaseName:      "enforcing_namespace_level_access_control",
				Input:         istioio.BookInfo("rbac/namespace-policy.yaml"),
				ResourceNames: []string{"service-viewer", "bind-service-viewer"},
			}, istioio.YamlResources{
				BaseName:      "enforcing_service_level_access_control_step1",
				Input:         istioio.BookInfo("rbac/productpage-policy.yaml"),
				ResourceNames: []string{"productpage-viewer", "bind-productpage-viewer"},
			}, istioio.YamlResources{
				BaseName:      "enforcing_service_level_access_control_step2",
				Input:         istioio.BookInfo("rbac/details-reviews-policy.yaml"),
				ResourceNames: []string{"details-reviews-viewer", "bind-details-reviews"},
			}, istioio.YamlResources{
				BaseName:      "enforcing_service_level_access_control_step3",
				Input:         istioio.BookInfo("rbac/ratings-policy.yaml"),
				ResourceNames: []string{"ratings-viewer", "bind-ratings"},
			}).
			// Remaining cleanup (undocumented).
			Defer(istioio.Script{
				Input: istioio.Inline{
					FileName: "cleanup.sh",
					Value: `
kubectl delete policy default -n default || true
kubectl delete destinationrule default -n default || true
kubectl delete clusterrbacconfig default || true
kubectl delete servicerole --all -n default || true
kubectl delete servicerolebinding --all -n default || true

kubectl delete -f samples/bookinfo/platform/kube/bookinfo.yaml || true
kubectl delete -f samples/bookinfo/networking/bookinfo-gateway.yaml || true
kubectl delete -f samples/sleep/sleep.yaml || true`,
				},
				WorkDir: env.IstioSrc,
			}).Build())
}
