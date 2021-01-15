package cluster

import (
	"istio.io/istio/pkg/test/framework/resource"
)

type Kind string

const (
	Kubernetes Kind = "Kubernetes"
	Aggregate  Kind = "Aggregate"
)

type Config struct {
	Kind                    Kind              `json:"kind,omitempty"`
	Name                    string            `json:"name,omitempty"`
	Network                 string            `json:"network,omitempty"`
	ControlPlaneClusterName string            `json:"controlPlaneClusterName,omitempty"`
	ConfigClusterName       string            `json:"configClusterName,omitempty"`
	Meta                    map[string]string `json:"meta,omitempty"`
}

type Factory interface {
	Kind() Kind
	With(config ...Config) Factory
	Build() (resource.Clusters, error)
}
