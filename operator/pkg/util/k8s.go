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

package util

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/label"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
)

type JWTPolicy string

const (
	FirstPartyJWT JWTPolicy = "first-party-jwt"
	ThirdPartyJWT JWTPolicy = "third-party-jwt"
)

// DetectSupportedJWTPolicy queries the api-server to detect whether it has TokenRequest support
func DetectSupportedJWTPolicy(client kubernetes.Interface) (JWTPolicy, error) {
	_, s, err := client.Discovery().ServerGroupsAndResources()
	// This may fail if any api service is down. We should only fail if the specific API we care about failed
	if err != nil {
		if discovery.IsGroupDiscoveryFailedError(err) {
			derr := err.(*discovery.ErrGroupDiscoveryFailed)
			if _, f := derr.Groups[schema.GroupVersion{Group: "authentication.k8s.io", Version: "v1"}]; f {
				return "", err
			}
		} else {
			return "", err
		}
	}
	for _, res := range s {
		for _, api := range res.APIResources {
			// Appearance of this API indicates we do support third party jwt token
			if api.Name == "serviceaccounts/token" {
				return ThirdPartyJWT, nil
			}
		}
	}
	return FirstPartyJWT, nil
}

// GKString differs from default representation of GroupKind
func GKString(gvk schema.GroupKind) string {
	return fmt.Sprintf("%s/%s", gvk.Group, gvk.Kind)
}

// ValidateIOPCAConfig validates if the IstioOperator CA configs are applicable to the K8s cluster
func ValidateIOPCAConfig(client kube.Client, iop *iopv1alpha1.IstioOperator) error {
	globalI := iop.Spec.Values.AsMap()["global"]
	global, ok := globalI.(map[string]any)
	if !ok {
		// This means no explicit global configuration. Still okay
		return nil
	}
	ca, ok := global["pilotCertProvider"].(string)
	if !ok {
		// This means the default pilotCertProvider is being used
		return nil
	}
	if ca == "kubernetes" {
		ver, err := client.GetKubernetesVersion()
		if err != nil {
			return fmt.Errorf("failed to determine support for K8s legacy signer. Use the --force flag to ignore this: %v", err)
		}

		if kube.IsAtLeastVersion(client, 22) {
			return fmt.Errorf("configuration PILOT_CERT_PROVIDER=%s not supported in Kubernetes %v."+
				"Please pick another value for PILOT_CERT_PROVIDER", ca, ver.String())
		}
	}
	return nil
}

// CreateNamespace creates a namespace using the given k8s interface.
func CreateNamespace(cs kubernetes.Interface, namespace string, network string, dryRun bool) error {
	if dryRun {
		scope.Infof("Not applying Namespace %s because of dry run.", namespace)
		return nil
	}
	if namespace == "" {
		// Setup default namespace
		namespace = constants.IstioSystemNamespace
	}
	// check if the namespace already exists. If yes, do nothing. If no, create a new one.
	if _, err := cs.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{}); err != nil {
		if errors.IsNotFound(err) {
			ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Name:   namespace,
				Labels: map[string]string{},
			}}
			if network != "" {
				ns.Labels[label.TopologyNetwork.Name] = network
			}
			_, err := cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create namespace %v: %v", namespace, err)
			}

			return nil
		}

		return fmt.Errorf("failed to check if namespace %v exists: %v", namespace, err)
	}

	return nil
}
