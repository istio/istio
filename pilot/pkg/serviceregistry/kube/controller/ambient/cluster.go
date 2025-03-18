package ambient

import (
	"crypto/sha256"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/network"
)

var _ krt.ResourceNamer = Cluster{}

type Cluster struct {
	// ID of the cluster.
	ID cluster.ID
	// Client for accessing the cluster.
	Client         kube.Client
	ClusterDetails krt.Singleton[ClusterDetails]

	// TODO: Figure out if we really need this and how to use it in krt
	KubeConfigSha [sha256.Size]byte
	Filter        kubetypes.DynamicObjectFilter
}

type ClusterDetails struct {
	SystemNamespace string
	Network         network.ID
}

func (c Cluster) ResourceName() string {
	return c.ID.String()
}

func (c ClusterDetails) ResourceName() string {
	return c.SystemNamespace
}
