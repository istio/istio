package kube

import (
	"context"
	
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/pkg/log"
)

type ClusterMeta struct {
	ID      string
	Network string
}

// ClusterMetaFromConfigMap attempts to load the istio multicluster config to get overrides for cluster and network names.
func ClusterMetaFromConfigMap(client kubernetes.Interface, namespace string) *ClusterMeta {
	log.Infof("looking for istio-cluster vm in namespace %s", namespace)
	cm, err := client.CoreV1().ConfigMaps(namespace).Get(context.TODO(), "istio-cluster", metaV1.GetOptions{})
	if err != nil {
		log.Errorf("error fetching istio-cluster configmap: %v", err)
		return nil
	}

	return &ClusterMeta{ID: cm.Data["cluster"], Network: cm.Data["network"]}
}
