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

package filesecrets

import (
	"fmt"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pkg/kube/krt"
	krtfiles "istio.io/istio/pkg/kube/krt/files"
)

type kubeconfigFile struct {
	clusterID  string
	kubeconfig []byte
}

// KubeconfigEntry represents a kubeconfig loaded from disk.
type KubeconfigEntry struct {
	Name       string
	Namespace  string
	ClusterID  string
	Kubeconfig []byte
}

func (k KubeconfigEntry) ResourceName() string {
	if k.Namespace == "" {
		return k.Name
	}
	return k.Namespace + "/" + k.Name
}

// NewKubeconfigCollection builds a file-backed collection of kubeconfigs.
// Each file is treated as a kubeconfig; its cluster ID is extracted from the
// kubeconfig contents and used as the entry name.
func NewKubeconfigCollection(
	root string,
	namespace string,
	opts ...krt.CollectionOption,
) (krt.Collection[KubeconfigEntry], error) {
	stop := krt.GetStop(opts...)
	fw, err := krtfiles.NewFolderWatch[kubeconfigFile](root, parseKubeconfig, stop)
	if err != nil {
		return nil, err
	}

	collection := krtfiles.NewFileCollection[kubeconfigFile, KubeconfigEntry](fw, func(k kubeconfigFile) *KubeconfigEntry {
		return &KubeconfigEntry{
			Name:       k.clusterID,
			Namespace:  namespace,
			ClusterID:  k.clusterID,
			Kubeconfig: k.kubeconfig,
		}
	}, opts...)

	return collection, nil
}

func parseKubeconfig(data []byte) ([]kubeconfigFile, error) {
	cfg, err := clientcmd.Load(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %v", err)
	}
	clusterID, err := clusterIDFromKubeconfig(cfg)
	if err != nil {
		return nil, err
	}
	return []kubeconfigFile{{
		clusterID:  clusterID,
		kubeconfig: data,
	}}, nil
}

func clusterIDFromKubeconfig(cfg *clientcmdapi.Config) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("kubeconfig is nil")
	}
	if cfg.CurrentContext != "" {
		if ctx, ok := cfg.Contexts[cfg.CurrentContext]; ok && ctx != nil && ctx.Cluster != "" {
			return ctx.Cluster, nil
		}
	}
	if len(cfg.Contexts) == 1 {
		for _, ctx := range cfg.Contexts {
			if ctx != nil && ctx.Cluster != "" {
				return ctx.Cluster, nil
			}
		}
	}
	if len(cfg.Clusters) == 1 {
		for name := range cfg.Clusters {
			if name != "" {
				return name, nil
			}
		}
	}
	return "", fmt.Errorf("unable to determine cluster ID from kubeconfig")
}
