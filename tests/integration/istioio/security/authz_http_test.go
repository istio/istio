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
	// Create an alias for getting a file in the script directory for this test.
	script := func(path string) istioio.Path {
		return istioio.Path("authz_http/" + path)
	}

	framework.
		NewTest(t).
		Run(istioio.NewBuilder("tasks__security__authorization_for_http_services").
			// Setup the initial environment.
			Add(istioio.Command{
				Input: istioio.Inline{
					FileName: "setup_namespace_and_policy.sh",
					Value: `
kubectl label namespace default istio-injection=enabled || true

cat <<EOF | kubectl apply -f -
apiVersion: authentication.istio.io/v1alpha1
kind: Policy
metadata:
  name: "default"
spec:
  peers:
  - mtls: {}
---
apiVersion: "networking.istio.io/v1alpha3"
kind: "DestinationRule"
metadata:
  name: "default"
spec:
  host: "*.default.svc.cluster.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
EOF`,
				},
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "setup_deploy.sh",
					Value: `
kubectl apply -f samples/bookinfo/platform/kube/bookinfo.yaml
kubectl apply -f samples/bookinfo/networking/bookinfo-gateway.yaml
kubectl apply -f samples/sleep/sleep.yaml

# Wait for the deployments to roll out.
for deploy in "productpage-v1" "details-v1" "ratings-v1" "reviews-v1" "reviews-v2" "reviews-v3" "sleep"; do
  if ! kubectl rollout status deployment "$deploy" --timeout 5m
  then
    echo "$deploy deployment rollout status check failed"
    exit 1
  fi
done

exit 0`,
				},
				WorkDir: env.IstioSrc,
			}).

			// Enabling Istio Authorization.
			Add(istioio.Command{
				Input: istioio.Inline{
					FileName: "enabling_istio_authorization.sh",
					Value:    "$ kubectl apply -f @samples/bookinfo/platform/kube/rbac/rbac-config-ON.yaml@",
				},
				WorkDir:       env.IstioSrc,
				CreateSnippet: true,
			}, istioio.Command{
				Input: script("enabling_istio_authorization_verify.sh"),
			}).

			// Enforcing Namespace-Level Access Control.
			Add(istioio.Command{
				Input: istioio.Inline{
					FileName: "enforcing_namespace_level_access_control_apply.sh",
					Value:    "$ kubectl apply -f @samples/bookinfo/platform/kube/rbac/namespace-policy.yaml@",
				},
				Verify: istioio.TokenVerifier(istioio.Inline{
					Value: `
servicerole.rbac.istio.io/service-viewer created
servicerolebinding.rbac.istio.io/bind-service-viewer created`,
				}),
				WorkDir:             env.IstioSrc,
				CreateSnippet:       true,
				CreateOutputSnippet: true,
			}, istioio.Command{
				Input: script("enforcing_namespace_level_access_control_verify.sh"),
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "enforcing_namespace_level_access_control_delete.sh",
					Value:    "$ kubectl delete -f @samples/bookinfo/platform/kube/rbac/namespace-policy.yaml@",
				},
				WorkDir:       env.IstioSrc,
				CreateSnippet: true,
			}, istioio.Command{
				// Verify that the delete puts us back to where we were before we applied the policy.
				Input: script("enabling_istio_authorization_verify.sh"),
			}, istioio.Snippet{
				// Create snippet for the service-viewer yaml resource.
				Name:   "enforcing_namespace_level_access_control_service_viewer.yaml",
				Syntax: "yaml",
				Input: istioio.YamlResource("service-viewer",
					istioio.BookInfo("rbac/namespace-policy.yaml")),
			}, istioio.Snippet{
				// Create snippet for the bind-service-viewer yaml resource.
				Name:   "enforcing_namespace_level_access_control_bind_service_viewer.yaml",
				Syntax: "yaml",
				Input: istioio.YamlResource("bind-service-viewer",
					istioio.BookInfo("rbac/namespace-policy.yaml")),
			}).

			// Enforcing Service-Level Access Control (Step 1).
			Add(istioio.Command{
				Input: istioio.Inline{
					FileName: "enforcing_service_level_access_control_step1_apply.sh",
					Value:    "$ kubectl apply -f @samples/bookinfo/platform/kube/rbac/productpage-policy.yaml@",
				},
				WorkDir:       env.IstioSrc,
				CreateSnippet: true,
			}, istioio.Command{
				Input: script("enforcing_service_level_access_control_step1_verify.sh"),
			}, istioio.Snippet{
				// Create snippet for the productpage-viewer resource.
				Name:   "enforcing_service_level_access_control_step1_productpage_viewer.yaml",
				Syntax: "yaml",
				Input: istioio.YamlResource("productpage-viewer",
					istioio.BookInfo("rbac/productpage-policy.yaml")),
			}, istioio.Snippet{
				// Create snippet for the bind-productpage-viewer resource.
				Name:   "enforcing_service_level_access_control_step1_bind_productpage_viewer.yaml",
				Syntax: "yaml",
				Input: istioio.YamlResource("bind-productpage-viewer",
					istioio.BookInfo("rbac/productpage-policy.yaml")),
			}).

			// Enforcing Service-Level Access Control (Step 2).
			Add(istioio.Command{
				Input: istioio.Inline{
					FileName: "enforcing_service_level_access_control_step2_apply.sh",
					Value:    "$ kubectl apply -f @samples/bookinfo/platform/kube/rbac/details-reviews-policy.yaml@",
				},
				WorkDir:       env.IstioSrc,
				CreateSnippet: true,
			}, istioio.Command{
				Input: script("enforcing_service_level_access_control_step2_verify.sh"),
			}, istioio.Snippet{
				// Create snippet for the details-reviews-viewer resource.
				Name:   "enforcing_service_level_access_control_step2_details_reviews_viewer.yaml",
				Syntax: "yaml",
				Input: istioio.YamlResource("details-reviews-viewer",
					istioio.BookInfo("rbac/details-reviews-policy.yaml")),
			}, istioio.Snippet{
				// Create snippet for the bind-productpage-viewer resource.
				Name:   "enforcing_service_level_access_control_step2_bind_details_reviews.yaml",
				Syntax: "yaml",
				Input: istioio.YamlResource("bind-details-reviews",
					istioio.BookInfo("rbac/details-reviews-policy.yaml")),
			}).

			// Enforcing Service-Level Access Control (Step 3).
			Add(istioio.Command{
				Input: istioio.Inline{
					FileName: "enforcing_service_level_access_control_step3_apply.sh",
					Value:    "$ kubectl apply -f @samples/bookinfo/platform/kube/rbac/ratings-policy.yaml@",
				},
				WorkDir:       env.IstioSrc,
				CreateSnippet: true,
			}, istioio.Command{
				Input: script("enforcing_service_level_access_control_step3_verify.sh"),
			}, istioio.Snippet{
				// Create snippet for the details-reviews-viewer resource.
				Name:   "enforcing_service_level_access_control_step3_ratings_viewer.yaml",
				Syntax: "yaml",
				Input: istioio.YamlResource("ratings-viewer",
					istioio.BookInfo("rbac/ratings-policy.yaml")),
			}, istioio.Snippet{
				// Create snippet for the bind-productpage-viewer resource.
				Name:   "enforcing_service_level_access_control_step3_bind_ratings.yaml",
				Syntax: "yaml",
				Input: istioio.YamlResource("bind-ratings",
					istioio.BookInfo("rbac/ratings-policy.yaml")),
			}).

			// Remove Istio authorization policy.
			Add(istioio.Command{
				Input: istioio.Inline{
					FileName: "remove_istio_authorization_policy.sh",
					Value: `
$ kubectl delete -f @samples/bookinfo/platform/kube/rbac/ratings-policy.yaml@
$ kubectl delete -f @samples/bookinfo/platform/kube/rbac/details-reviews-policy.yaml@
$ kubectl delete -f @samples/bookinfo/platform/kube/rbac/productpage-policy.yaml@`,
				},
				WorkDir:       env.IstioSrc,
				CreateSnippet: true,
			}).

			// Remove Istio authorization policy (alternative).
			Add(istioio.Command{
				Input: istioio.Inline{
					FileName: "remove_istio_authorization_policy_alternative.sh",
					Value: `
$ kubectl delete servicerole --all
$ kubectl delete servicerolebinding --all`,
				},
				WorkDir:       env.IstioSrc,
				CreateSnippet: true,
			}).

			// Disable Istio authorization policy.
			Add(istioio.Command{
				Input: istioio.Inline{
					FileName: "disabling_istio_authorization.sh",
					Value:    "$ kubectl delete -f @samples/bookinfo/platform/kube/rbac/rbac-config-ON.yaml@",
				},
				WorkDir:       env.IstioSrc,
				CreateSnippet: true,
			}).

			// Remaining cleanup (undocumented).
			Defer(istioio.Command{
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
