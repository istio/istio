package leaderelection

import (
	"fmt"
	"strings"

	"istio.io/istio/pilot/pkg/features"
)

const prefix = "istio-"

func BuildClusterScopedLeaderElection(original string) string {
	result := original
	if features.ClusterName != "" && features.ClusterName != "Kubernetes" {
		suffix := strings.TrimPrefix(original, prefix)
		return fmt.Sprintf("%s-%s", features.ClusterName, suffix)
	}

	return result
}
