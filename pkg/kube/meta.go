// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kube

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"

	"istio.io/pkg/log"
)

const IstioClusterConfigMap = "istio-cluster"

type ClusterMeta struct {
	// Network is the default network to use for pods/endpoints in a given cluster if neither meshNetworks.fromRegistry
	// for the cluster, nor topology.istio.io/network on pods are set.
	Network string
}

func configMapName(revision string) string {
	if revision == "" {
		return IstioClusterConfigMap
	}
	return IstioClusterConfigMap + "-" + revision
}

func WatchClusterMeta(client kubernetes.Interface, namespace, revision string) (watch.Interface, error) {
	w, err := client.CoreV1().ConfigMaps(namespace).
		Watch(context.TODO(), metaV1.ListOptions{FieldSelector: "metadata.name=" + configMapName(revision)})
	if err != nil {
		return nil, err
	}
	return w, err
}

// FetchClusterMeta attempts to load the istio multicluster config to get overrides for cluster and network names.
func FetchClusterMeta(client kubernetes.Interface, namespace, revision string) *ClusterMeta {
	cm, err := client.CoreV1().ConfigMaps(namespace).Get(context.TODO(), configMapName(revision), metaV1.GetOptions{})
	if err != nil {
		log.Errorf("error fetching istio-cluster ConfigMap: %v", err)
		return nil
	}

	return ClusterMetaFromConfigMap(cm)
}

func ClusterMetaFromConfigMap(cm *v1.ConfigMap) *ClusterMeta {
	if cm == nil {
		return nil
	}
	return &ClusterMeta{Network: cm.Data["network"]}
}
