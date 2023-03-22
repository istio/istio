package constants

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
)

var IstioOperatorGVR = schema.GroupVersionResource{
	Group:    v1alpha1.SchemeGroupVersion.Group,
	Version:  v1alpha1.SchemeGroupVersion.Version,
	Resource: "istiooperators",
}
