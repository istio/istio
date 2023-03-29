package ambient

import "strings"

func IsZtunnelPod(podName string) bool {
	return strings.HasPrefix(podName, "ztunnel")
}
