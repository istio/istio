package ambient

import (
	"context"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/util/sets"
	corev1 "k8s.io/api/core/v1"
)

type PluginType string

const (
	PluginTypeCilium PluginType = "cilium"
)

// Plugin is an interface for ambient plugins.
// Allows to use other CNIs as plugins.
// (e.g. Cilium)
type Plugin interface {
	UpdateHostIP(hostIps []string) error
	UpdateNodeProxy(pod *corev1.Pod, dns bool)
	DumpEnrolledIPs() sets.Set[string]
	DelPodOnNode(ip string) error
	UpdatePodOnNode(pod *corev1.Pod) error
	DelZTunnel() error
	CleanupPodsOnNode() error
}

// PluginFactory is a factory for ambient plugins.
type PluginFactory interface {
	// Create creates a new plugin.
	Create(ctx context.Context, kubeClient kube.Client) (Plugin, error)
}

func GetFactory(pluginName PluginType) PluginFactory {
	switch pluginName {
	case PluginTypeCilium:
		return nil
	default:
		return nil
	}
}
