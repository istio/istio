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

package multicluster

import (
	"fmt"
	"sort"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pkg/kube/krt"
	krtfiles "istio.io/istio/pkg/kube/krt/files"
)

// KubeconfigFile represents a kubeconfig loaded from disk.
type KubeconfigFile struct {
	ClusterID  string
	Kubeconfig []byte
}

func (k KubeconfigFile) ResourceName() string {
	return k.ClusterID
}

// NewKubeconfigCollection builds a file-backed collection of kubeconfigs.
// Each file is treated as a kubeconfig; its cluster ID is extracted from the
// kubeconfig contents and used as the entry name.
func NewKubeconfigCollection(
	root string,
	opts ...krt.CollectionOption,
) (krt.Collection[KubeconfigFile], error) {
	stop := krt.GetStop(opts...)
	fw, err := krtfiles.NewFolderWatch[KubeconfigFile](root, parseKubeconfig, stop)
	if err != nil {
		return nil, err
	}

	collection := krtfiles.NewFileCollection[KubeconfigFile, KubeconfigFile](fw, func(k KubeconfigFile) *KubeconfigFile {
		return &KubeconfigFile{
			ClusterID:  k.ClusterID,
			Kubeconfig: k.Kubeconfig,
		}
	}, opts...)

	return collection, nil
}

func parseKubeconfig(data []byte) ([]KubeconfigFile, error) {
	cfg, err := clientcmd.Load(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %v", err)
	}
	clusterID, err := clusterIDFromKubeconfig(cfg)
	if err != nil {
		return nil, err
	}
	return []KubeconfigFile{{
		ClusterID:  clusterID,
		Kubeconfig: data,
	}}, nil
}

// clusterIDFromKubeconfig selects a cluster ID in this order:
// 1. The cluster referenced by the current context.
// 2. The first valid context by lexicographic context name.
// 3. The first cluster name by lexicographic order.
func clusterIDFromKubeconfig(cfg *clientcmdapi.Config) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("kubeconfig is nil")
	}

	if ctx, ok := cfg.Contexts[cfg.CurrentContext]; ok && ctx != nil && ctx.Cluster != "" {
		return ctx.Cluster, nil
	}

	contextNames := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		contextNames = append(contextNames, name)
	}
	// Sort map keys so fallback selection is stable across process restarts.
	sort.Strings(contextNames)
	for _, name := range contextNames {
		ctx := cfg.Contexts[name]
		if ctx != nil && ctx.Cluster != "" {
			return ctx.Cluster, nil
		}
	}

	clusterNames := make([]string, 0, len(cfg.Clusters))
	for name := range cfg.Clusters {
		if name == "" {
			continue
		}
		clusterNames = append(clusterNames, name)
	}
	// Sort map keys so fallback selection is stable across process restarts.
	sort.Strings(clusterNames)
	if len(clusterNames) > 0 {
		return clusterNames[0], nil
	}

	return "", fmt.Errorf("unable to determine cluster ID from kubeconfig")
}
