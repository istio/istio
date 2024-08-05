package credentials

import (
	"fmt"
	"strings"

	"istio.io/istio/pkg/cluster"
)

const (
	KubernetesIngressSecretType    = "kubernetes-ingress"
	KubernetesIngressSecretTypeURI = KubernetesIngressSecretType + "://"
)

func ToKubernetesIngressResourceBasedFullName(fullName string) string {
	return fmt.Sprintf("%s://%s", KubernetesIngressSecretType, fullName)
}

func ToKubernetesIngressResource(clusterId, namespace, name string) string {
	return fmt.Sprintf("%s://%s/%s/%s", KubernetesIngressSecretType, clusterId, namespace, name)
}

func createSecretResourceForIngress(resourceName string) (SecretResource, error) {
	res := strings.TrimPrefix(resourceName, KubernetesIngressSecretTypeURI)
	split := strings.Split(res, "/")
	if len(split) != 3 {
		return SecretResource{}, fmt.Errorf("invalid resource name %q. Expected clusterId, namespace and name", resourceName)
	}
	clusterId := split[0]
	namespace := split[1]
	name := split[2]
	if len(clusterId) == 0 {
		return SecretResource{}, fmt.Errorf("invalid resource name %q. Expected clusterId", resourceName)
	}
	if len(namespace) == 0 {
		return SecretResource{}, fmt.Errorf("invalid resource name %q. Expected namespace", resourceName)
	}
	if len(name) == 0 {
		return SecretResource{}, fmt.Errorf("invalid resource name %q. Expected name", resourceName)
	}
	return SecretResource{ResourceType: KubernetesIngressSecretType, Name: name, Namespace: namespace, ResourceName: resourceName, Cluster: cluster.ID(clusterId)}, nil
}
