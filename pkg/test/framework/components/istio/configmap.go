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

package istio

import (
	"context"
	"fmt"
	"sync"

	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/cleanup"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/util/protomarshal"
)

type configMap struct {
	ctx        resource.Context
	namespace  string
	mu         sync.Mutex
	meshConfig *meshconfig.MeshConfig
	revisions  resource.RevVerMap
}

func newConfigMap(ctx resource.Context, namespace string, revisions resource.RevVerMap) *configMap {
	return &configMap{
		ctx:       ctx,
		namespace: namespace,
		revisions: revisions,
	}
}

func (cm *configMap) MeshConfig() (*meshconfig.MeshConfig, error) {
	cm.mu.Lock()
	mc := cm.meshConfig
	cm.mu.Unlock()

	if mc == nil {
		c := cm.ctx.AllClusters().Configs()[0]

		cfgMap, err := cm.getConfigMap(c)
		if err != nil {
			return nil, err
		}

		// Get the MeshConfig yaml from the config map.
		mcYAML, err := getMeshConfigData(c, cfgMap)
		if err != nil {
			return nil, err
		}

		// Parse the YAML.
		mc, err = yamlToMeshConfig(mcYAML)
		if err != nil {
			return nil, err
		}

		// Save the updated mesh config.
		cm.mu.Lock()
		cm.meshConfig = mc
		cm.mu.Unlock()
	}

	return mc, nil
}

func (cm *configMap) MeshConfigOrFail(t test.Failer) *meshconfig.MeshConfig {
	t.Helper()
	out, err := cm.MeshConfig()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (cm *configMap) UpdateMeshConfig(t resource.Context, update func(*meshconfig.MeshConfig) error, cleanupStrategy cleanup.Strategy) error {
	// Invalidate the member variable. The next time it's requested, it will get a fresh value.
	cm.mu.Lock()
	cm.meshConfig = nil
	cm.mu.Unlock()

	errG := multierror.Group{}
	origCfg := map[string]string{}
	mu := sync.RWMutex{}

	for _, c := range cm.ctx.AllClusters().Kube() {
		c := c
		errG.Go(func() error {
			cfgMap, err := cm.getConfigMap(c)
			if err != nil {
				return err
			}

			// Get the MeshConfig yaml from the config map.
			mcYAML, err := getMeshConfigData(c, cfgMap)
			if err != nil {
				return err
			}

			// Store the original YAML so we can restore later.
			mu.Lock()
			origCfg[c.Name()] = mcYAML
			mu.Unlock()

			// Parse the YAML.
			mc, err := yamlToMeshConfig(mcYAML)
			if err != nil {
				return err
			}

			// Apply the change.
			if err := update(mc); err != nil {
				return err
			}

			// Store the updated MeshConfig back into the config map.
			newYAML, err := meshConfigToYAML(mc)
			if err != nil {
				return err
			}
			setMeshConfigData(cfgMap, newYAML)

			// Write the config map back to the cluster.
			if err := cm.updateConfigMap(c, cfgMap); err != nil {
				return err
			}
			scopes.Framework.Infof("patched %s meshconfig:\n%s", c.Name(), cfgMap.Data["mesh"])
			return nil
		})
	}

	// Restore the original value of the MeshConfig when the context completes.
	t.CleanupStrategy(cleanupStrategy, func() {
		// Invalidate the member mesh config again, since we're rolling back the changes.
		cm.mu.Lock()
		cm.meshConfig = nil
		cm.mu.Unlock()

		errG := multierror.Group{}
		mu.RLock()
		defer mu.RUnlock()
		for cn, mcYAML := range origCfg {
			cn, mcYAML := cn, mcYAML
			c := cm.ctx.AllClusters().GetByName(cn)
			errG.Go(func() error {
				cfgMap, err := cm.getConfigMap(c)
				if err != nil {
					return err
				}
				setMeshConfigData(cfgMap, mcYAML)
				if err := cm.updateConfigMap(c, cfgMap); err != nil {
					return err
				}
				scopes.Framework.Infof("cleanup patched %s meshconfig:\n%s", c.Name(), cfgMap.Data["mesh"])
				return nil
			})
		}
		if err := errG.Wait().ErrorOrNil(); err != nil {
			scopes.Framework.Errorf("failed cleaning up cluster-local config: %v", err)
		}
	})
	return errG.Wait().ErrorOrNil()
}

func (cm *configMap) UpdateMeshConfigOrFail(ctx resource.Context, t test.Failer, update func(*meshconfig.MeshConfig) error, cleanupStrategy cleanup.Strategy) {
	t.Helper()
	if err := cm.UpdateMeshConfig(ctx, update, cleanupStrategy); err != nil {
		t.Fatal(err)
	}
}

func (cm *configMap) PatchMeshConfig(t resource.Context, patch string) error {
	return cm.UpdateMeshConfig(t, func(mc *meshconfig.MeshConfig) error {
		return protomarshal.ApplyYAML(patch, mc)
	}, cleanup.Always)
}

func (cm *configMap) PatchMeshConfigOrFail(ctx resource.Context, t test.Failer, patch string) {
	t.Helper()
	if err := cm.PatchMeshConfig(ctx, patch); err != nil {
		t.Fatal(err)
	}
}

func (cm *configMap) configMapName() string {
	cmName := "istio"
	if rev := cm.revisions.Default(); rev != "default" && rev != "" {
		cmName += "-" + rev
	}
	return cmName
}

func (cm *configMap) getConfigMap(c cluster.Cluster) (*corev1.ConfigMap, error) {
	return c.Kube().CoreV1().ConfigMaps(cm.namespace).Get(context.TODO(), cm.configMapName(), metav1.GetOptions{})
}

func (cm *configMap) updateConfigMap(c cluster.Cluster, cfgMap *corev1.ConfigMap) error {
	_, err := c.Kube().CoreV1().ConfigMaps(cm.namespace).Update(context.TODO(), cfgMap, metav1.UpdateOptions{})
	return err
}

func getMeshConfigData(c cluster.Cluster, cm *corev1.ConfigMap) (string, error) {
	// Get the MeshConfig yaml from the config map.
	mcYAML, ok := cm.Data["mesh"]
	if !ok {
		return "", fmt.Errorf("mesh config was missing in istio config map for %s", c.Name())
	}
	return mcYAML, nil
}

func setMeshConfigData(cm *corev1.ConfigMap, mcYAML string) {
	cm.Data["mesh"] = mcYAML
}

func yamlToMeshConfig(mcYAML string) (*meshconfig.MeshConfig, error) {
	// Parse the YAML.
	mc := &meshconfig.MeshConfig{}
	if err := protomarshal.ApplyYAML(mcYAML, mc); err != nil {
		return nil, err
	}
	return mc, nil
}

func meshConfigToYAML(mc *meshconfig.MeshConfig) (string, error) {
	return protomarshal.ToYAML(mc)
}

func (cm *configMap) updateMeshConfig(c cluster.Cluster, cfgMap *corev1.ConfigMap, mc *meshconfig.MeshConfig) error {
	// Store the updated MeshConfig back into the config map.
	var err error
	cfgMap.Data["mesh"], err = meshConfigToYAML(mc)
	if err != nil {
		return err
	}

	// Write the config map back to the cluster.
	return cm.updateConfigMap(c, cfgMap)
}
