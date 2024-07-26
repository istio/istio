package multicluster

import (
	"strconv"
	"strings"
)

type ClusterInfo struct {
	EnableIngress       bool
	ClusterID           string
	IngressClass        string
	WatchNamespace      string
	EnableIngressStatus bool
}

// ConvertToClusterInfo obtain cluster info from cluster key.
// The cluster key format is k8sClusterId ingressClass watchNamespace EnableStatus, delimited by _.
func ConvertToClusterInfo(key string) ClusterInfo {
	parts := strings.Split(key, "_")
	// Old cluster key
	if len(parts) < 3 {
		return ClusterInfo{
			ClusterID: parts[0],
		}
	}

	clusterInfo := ClusterInfo{
		EnableIngress:  true,
		ClusterID:      parts[0],
		IngressClass:   parts[1],
		WatchNamespace: parts[2],
		// The status switch is enabled by default.
		EnableIngressStatus: true,
	}

	if len(parts) == 4 {
		if enable, err := strconv.ParseBool(parts[3]); err == nil {
			clusterInfo.EnableIngressStatus = enable
		}
	}
	return clusterInfo
}

func (c ClusterInfo) Equal(target ClusterInfo) bool {
	// We don't care about ingress status.
	c.EnableIngressStatus = target.EnableIngressStatus
	return c == target
}
