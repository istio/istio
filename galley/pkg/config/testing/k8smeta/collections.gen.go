// GENERATED FILE -- DO NOT EDIT
//

package k8smeta

import (
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
)

var (

	// K8SAppsV1Deployments is the name of collection k8s/apps/v1/deployments
	K8SAppsV1Deployments = collection.NewName("k8s/apps/v1/deployments")

	// K8SCoreV1Endpoints is the name of collection k8s/core/v1/endpoints
	K8SCoreV1Endpoints = collection.NewName("k8s/core/v1/endpoints")

	// K8SCoreV1Namespaces is the name of collection k8s/core/v1/namespaces
	K8SCoreV1Namespaces = collection.NewName("k8s/core/v1/namespaces")

	// K8SCoreV1Nodes is the name of collection k8s/core/v1/nodes
	K8SCoreV1Nodes = collection.NewName("k8s/core/v1/nodes")

	// K8SCoreV1Pods is the name of collection k8s/core/v1/pods
	K8SCoreV1Pods = collection.NewName("k8s/core/v1/pods")

	// K8SCoreV1Services is the name of collection k8s/core/v1/services
	K8SCoreV1Services = collection.NewName("k8s/core/v1/services")

	// K8SExtensionsV1Beta1Ingresses is the name of collection k8s/extensions/v1beta1/ingresses
	K8SExtensionsV1Beta1Ingresses = collection.NewName("k8s/extensions/v1beta1/ingresses")
)

// CollectionNames returns the collection names declared in this package.
func CollectionNames() []collection.Name {
	return []collection.Name{
		K8SAppsV1Deployments,
		K8SCoreV1Endpoints,
		K8SCoreV1Namespaces,
		K8SCoreV1Nodes,
		K8SCoreV1Pods,
		K8SCoreV1Services,
		K8SExtensionsV1Beta1Ingresses,
	}
}
