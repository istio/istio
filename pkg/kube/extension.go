package kube

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const ClusterExtensionKey = "istio"

var _ runtime.Object = ClusterExtension{}

type ClusterExtension struct {
	Network string `json:"network,omitempty"`
}

func (c ClusterExtension) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind
}

func (c ClusterExtension) DeepCopyObject() runtime.Object {
	return c
}
