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

package credentials

import (
	"fmt"
	"strings"

	"istio.io/istio/pkg/cluster"
)

const (
	// KubernetesSecretType is the name of a SDS secret stored in Kubernetes. Secrets here take the form
	// kubernetes://secret-name. They will be pulled from the same namespace and cluster as the requesting proxy lives in.
	KubernetesSecretType    = "kubernetes"
	KubernetesSecretTypeURI = KubernetesSecretType + "://"
	// KubernetesGatewaySecretType is the name of a SDS secret stored in Kubernetes, used by the gateway-api. Secrets here
	// take the form kubernetes-gateway://namespace/name. They are pulled from the config cluster.
	KubernetesGatewaySecretType    = "kubernetes-gateway"
	kubernetesGatewaySecretTypeURI = KubernetesGatewaySecretType + "://"
	// BuiltinGatewaySecretType is the name of a SDS secret that uses the workloads own mTLS certificate
	BuiltinGatewaySecretType    = "builtin"
	BuiltinGatewaySecretTypeURI = BuiltinGatewaySecretType + "://"
)

// SecretResource defines a reference to a secret
type SecretResource struct {
	// Type is the type of secret. One of KubernetesSecretType or KubernetesGatewaySecretType
	Type string
	// Name is the name of the secret
	Name string
	// Namespace is the namespace the secret resides in. For implicit namespace references (such as in KubernetesSecretType),
	// this will be resolved to the appropriate namespace. As a result, this should never be empty.
	Namespace string
	// ResourceName is the original name of the resource
	ResourceName string
	// Cluster is the cluster the secret should be fetched from.
	Cluster cluster.ID
}

func (sr SecretResource) Key() string {
	return "sds://" + sr.Type + "/" + sr.Name + "/" + sr.Namespace + "/" + string(sr.Cluster)
}

func (sr SecretResource) KubernetesResourceName() string {
	return fmt.Sprintf("%s://%s/%s", sr.Type, sr.Namespace, sr.Name)
}

func ToKubernetesGatewayResource(namespace, name string) string {
	if strings.HasPrefix(name, BuiltinGatewaySecretTypeURI) {
		return BuiltinGatewaySecretTypeURI
	}
	return fmt.Sprintf("%s://%s/%s", KubernetesGatewaySecretType, namespace, name)
}

// ToResourceName turns a `credentialName` into a resource name used for SDS
func ToResourceName(name string) string {
	if strings.HasPrefix(name, BuiltinGatewaySecretTypeURI) {
		return "default"
	}
	// If they explicitly defined the type, keep it
	if strings.HasPrefix(name, KubernetesSecretTypeURI) || strings.HasPrefix(name, kubernetesGatewaySecretTypeURI) {
		return name
	}
	// Otherwise, to kubernetes://
	return KubernetesSecretTypeURI + name
}

// ParseResourceName parses a raw resourceName string.
func ParseResourceName(resourceName string, proxyNamespace string, proxyCluster cluster.ID, configCluster cluster.ID) (SecretResource, error) {
	sep := "/"
	if strings.HasPrefix(resourceName, KubernetesSecretTypeURI) {
		// Valid formats:
		// * kubernetes://secret-name
		// * kubernetes://secret-namespace/secret-name
		// If namespace is not set, we will fetch from the namespace of the proxy. The secret will be read from
		// the cluster the proxy resides in. This mirrors the legacy behavior mounting a secret as a file
		res := strings.TrimPrefix(resourceName, KubernetesSecretTypeURI)
		split := strings.Split(res, sep)
		namespace := proxyNamespace
		name := split[0]
		if len(split) > 1 {
			namespace = split[0]
			name = split[1]
		}
		return SecretResource{Type: KubernetesSecretType, Name: name, Namespace: namespace, ResourceName: resourceName, Cluster: proxyCluster}, nil
	} else if strings.HasPrefix(resourceName, kubernetesGatewaySecretTypeURI) {
		// Valid formats:
		// * kubernetes-gateway://secret-namespace/secret-name
		// Namespace is required. The secret is read from the config cluster; this is the primary difference from KubernetesSecretType.
		res := strings.TrimPrefix(resourceName, kubernetesGatewaySecretTypeURI)
		split := strings.Split(res, sep)
		if len(split) <= 1 {
			return SecretResource{}, fmt.Errorf("invalid resource name %q. Expected namespace and name", resourceName)
		}
		namespace := split[0]
		name := split[1]
		if len(namespace) == 0 {
			return SecretResource{}, fmt.Errorf("invalid resource name %q. Expected namespace", resourceName)
		}
		if len(name) == 0 {
			return SecretResource{}, fmt.Errorf("invalid resource name %q. Expected name", resourceName)
		}
		return SecretResource{Type: KubernetesGatewaySecretType, Name: name, Namespace: namespace, ResourceName: resourceName, Cluster: configCluster}, nil
	}
	return SecretResource{}, fmt.Errorf("unknown resource type: %v", resourceName)
}
