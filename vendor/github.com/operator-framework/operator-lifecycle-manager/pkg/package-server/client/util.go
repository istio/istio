package client

import (
	"net"
	"os"

	log "github.com/sirupsen/logrus"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// getConfig returns a kubernetes config for configuring a client from a kubeconfig string
func getConfig(kubeconfig string) (*rest.Config, error) {
	if len(kubeconfig) == 0 {
		// Work around https://github.com/kubernetes/kubernetes/issues/40973
		// See https://github.com/coreos/etcd-operator/issues/731#issuecomment-283804819
		if len(os.Getenv("KUBERNETES_SERVICE_HOST")) == 0 {
			addrs, err := net.LookupHost("kubernetes.default.svc")
			if err != nil {
				return nil, err
			}

			os.Setenv("KUBERNETES_SERVICE_HOST", addrs[0])
		}

		if len(os.Getenv("KUBERNETES_SERVICE_PORT")) == 0 {
			os.Setenv("KUBERNETES_SERVICE_PORT", "443")
		}

		log.Infof("Using in-cluster kube client config")
		return rest.InClusterConfig()
	}
	log.Infof("Loading kube client config from path %q", kubeconfig)
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}
