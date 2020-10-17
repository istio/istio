package kube

import (
	"context"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/pkg/log"
)

type ClusterMeta struct {
	ID      string
	Network string
}

// ClusterMetaFromConfigMap attempts to load the istio multicluster config to get overrides for cluster and network names.
func ClusterMetaFromConfigMap(client kubernetes.Interface, namespace string) *ClusterMeta {
	// TODO fix circular import for label const
	log.Infof("looking for istio-cluster vm in namespace %s", namespace)
	res, err := client.CoreV1().ConfigMaps(namespace).List(context.TODO(), v1.ListOptions{LabelSelector: "istio/multiCluster=true"})
	if err != nil {
		log.Errorf("failed fetching cluster meta configmap: %v", err)
		return nil
	}
	if len(res.Items) == 0 {
		log.Errorf("no matching configmap: %v", err)
		return nil
	}
	if len(res.Items) > 1 {
		log.Warnf("multiple ConfigMaps with istio/multiCluster=true; using %s", res.Items[0].Name)
	}
	cm := res.Items[0]

	return &ClusterMeta{ID: cm.Data["cluster"], Network: cm.Data["network"]}
}
