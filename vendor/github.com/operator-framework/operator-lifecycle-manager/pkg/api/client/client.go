package client

import (
	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned"
	"k8s.io/client-go/rest"
)

// NewClient creates a client that can interact with the ALM resources in k8s api
func NewClient(kubeconfig string) (client versioned.Interface, err error) {
	var config *rest.Config
	config, err = getConfig(kubeconfig)
	if err != nil {
		return
	}
	return versioned.NewForConfig(config)
}
