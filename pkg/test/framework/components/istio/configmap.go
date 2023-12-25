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
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"sync"

	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/cleanup"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/util/protomarshal"
)

type configMap struct {
	ctx       resource.Context
	namespace string
	mu        sync.Mutex
	revisions resource.RevVerMap
}

type meshConfig struct {
	configMap
	meshConfig *meshconfig.MeshConfig
}

type injectConfig struct {
	configMap
	injectConfig *inject.Config
	values       *inject.ValuesConfig
}

func newConfigMap(ctx resource.Context, namespace string, revisions resource.RevVerMap) *configMap {
	return &configMap{
		ctx:       ctx,
		namespace: namespace,
		revisions: revisions,
	}
}

func (ic *injectConfig) InjectConfig() (*inject.Config, error) {
	ic.mu.Lock()
	myic := ic.injectConfig
	ic.mu.Unlock()

	if myic == nil {
		c := ic.ctx.AllClusters().Configs()[0]

		cfgMap, err := ic.getConfigMap(c, ic.configMapName())
		if err != nil {
			return nil, err
		}

		// Get the MeshConfig yaml from the config map.
		icYAML, err := getInjectConfigYaml(cfgMap, "config")
		if err != nil {
			return nil, err
		}

		// Parse the YAML.
		myic, err = yamlToInjectConfig(icYAML)
		if err != nil {
			return nil, err
		}

		// Save the updated mesh config.
		ic.mu.Lock()
		ic.injectConfig = myic
		ic.mu.Unlock()
	}

	return myic, nil
}

func (ic *injectConfig) UpdateInjectionConfig(t resource.Context, update func(*inject.Config) error, cleanupStrategy cleanup.Strategy) error {
	// Invalidate the member variable. The next time it's requested, it will get a fresh value.
	ic.mu.Lock()
	ic.injectConfig = nil
	ic.mu.Unlock()

	errG := multierror.Group{}
	origCfg := map[string]string{}
	mu := sync.RWMutex{}

	for _, c := range ic.ctx.AllClusters().Kube() {
		c := c
		errG.Go(func() error {
			cfgMap, err := ic.getConfigMap(c, ic.configMapName())
			if err != nil {
				return err
			}

			// Get the MeshConfig yaml from the config map.
			mcYAML, err := getInjectConfigYaml(cfgMap, "config")
			if err != nil {
				return err
			}

			// Store the original YAML so we can restore later.
			mu.Lock()
			origCfg[c.Name()] = mcYAML
			mu.Unlock()

			// Parse the YAML.
			mc, err := yamlToInjectConfig(mcYAML)
			if err != nil {
				return err
			}

			// Apply the change.
			if err := update(mc); err != nil {
				return err
			}

			// Store the updated MeshConfig back into the config map.
			newYAML, err := injectConfigToYaml(mc)
			if err != nil {
				return err
			}
			cfgMap.Data["config"] = newYAML

			// Write the config map back to the cluster.
			if err := ic.updateConfigMap(c, cfgMap); err != nil {
				return err
			}
			scopes.Framework.Debugf("patched %s injection configmap:\n%s", c.Name(), cfgMap.Data["config"])
			return nil
		})
	}

	// Restore the original value of the MeshConfig when the context completes.
	t.CleanupStrategy(cleanupStrategy, func() {
		// Invalidate the member mesh config again, since we're rolling back the changes.
		ic.mu.Lock()
		ic.injectConfig = nil
		ic.mu.Unlock()

		errG := multierror.Group{}
		mu.RLock()
		defer mu.RUnlock()
		for cn, mcYAML := range origCfg {
			cn, mcYAML := cn, mcYAML
			c := ic.ctx.AllClusters().GetByName(cn)
			errG.Go(func() error {
				cfgMap, err := ic.getConfigMap(c, ic.configMapName())
				if err != nil {
					return err
				}

				cfgMap.Data["config"] = mcYAML
				if err := ic.updateConfigMap(c, cfgMap); err != nil {
					return err
				}
				scopes.Framework.Debugf("cleanup patched %s injection configmap:\n%s", c.Name(), cfgMap.Data["config"])
				return nil
			})
		}
		if err := errG.Wait().ErrorOrNil(); err != nil {
			scopes.Framework.Errorf("failed cleaning up cluster-local config: %v", err)
		}
	})
	return errG.Wait().ErrorOrNil()
}

func (ic *injectConfig) ValuesConfig() (*inject.ValuesConfig, error) {
	ic.mu.Lock()
	myic := ic.values
	ic.mu.Unlock()

	if myic == nil {
		c := ic.ctx.AllClusters().Configs()[0]

		cfgMap, err := ic.getConfigMap(c, ic.configMapName())
		if err != nil {
			return nil, err
		}

		// Get the MeshConfig yaml from the config map.
		icYAML, err := getInjectConfigYaml(cfgMap, "values")
		if err != nil {
			return nil, err
		}

		// Parse the YAML.
		s, err := inject.NewValuesConfig(icYAML)
		if err != nil {
			return nil, err
		}
		myic = &s

		// Save the updated mesh config.
		ic.mu.Lock()
		ic.values = myic
		ic.mu.Unlock()
	}

	return myic, nil
}

func (ic *injectConfig) configMapName() string {
	cmName := "istio-sidecar-injector"
	if rev := ic.revisions.Default(); rev != "default" && rev != "" {
		cmName += "-" + rev
	}
	return cmName
}

func (mc *meshConfig) MeshConfig() (*meshconfig.MeshConfig, error) {
	mc.mu.Lock()
	mymc := mc.meshConfig
	mc.mu.Unlock()

	if mymc == nil {
		c := mc.ctx.AllClusters().Configs()[0]

		cfgMapName, err := mc.configMapName()
		if err != nil {
			return nil, err
		}

		cfgMap, err := mc.getConfigMap(c, cfgMapName)
		if err != nil {
			return nil, err
		}

		// Get the MeshConfig yaml from the config map.
		mcYAML, err := getMeshConfigData(c, cfgMap)
		if err != nil {
			return nil, err
		}

		// Parse the YAML.
		mymc, err = yamlToMeshConfig(mcYAML)
		if err != nil {
			return nil, err
		}

		// Save the updated mesh config.
		mc.mu.Lock()
		mc.meshConfig = mymc
		mc.mu.Unlock()
	}

	return mymc, nil
}

func (mc *meshConfig) MeshConfigOrFail(t test.Failer) *meshconfig.MeshConfig {
	t.Helper()
	out, err := mc.MeshConfig()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (mc *meshConfig) UpdateMeshConfig(t resource.Context, update func(*meshconfig.MeshConfig) error, cleanupStrategy cleanup.Strategy) error {
	// Invalidate the member variable. The next time it's requested, it will get a fresh value.
	mc.mu.Lock()
	mc.meshConfig = nil
	mc.mu.Unlock()

	errG := multierror.Group{}
	origCfg := map[string]string{}
	mu := sync.RWMutex{}

	for _, c := range mc.ctx.AllClusters().Kube() {
		c := c
		errG.Go(func() error {
			cfgMapName, err := mc.configMapName()
			if err != nil {
				return err
			}

			cfgMap, err := mc.getConfigMap(c, cfgMapName)
			if err != nil {
				// Remote clusters typically don't have mesh config, allow it to skip
				if c.IsRemote() && kerrors.IsNotFound(err) {
					scopes.Framework.Infof("skipped %s meshconfig patch, as it is a remote", c.Name())
					return nil
				}
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
			ymc, err := yamlToMeshConfig(mcYAML)
			if err != nil {
				return err
			}

			// Apply the change.
			if err := update(ymc); err != nil {
				return err
			}

			// Store the updated MeshConfig back into the config map.
			newYAML, err := meshConfigToYAML(ymc)
			if err != nil {
				return err
			}
			setMeshConfigData(cfgMap, newYAML)

			// Write the config map back to the cluster.
			if err := mc.updateConfigMap(c, cfgMap); err != nil {
				return err
			}
			scopes.Framework.Infof("patched %s meshconfig:\n%s", c.Name(), cfgMap.Data["mesh"])
			return nil
		})
	}

	// Restore the original value of the MeshConfig when the context completes.
	t.CleanupStrategy(cleanupStrategy, func() {
		// Invalidate the member mesh config again, since we're rolling back the changes.
		mc.mu.Lock()
		mc.meshConfig = nil
		mc.mu.Unlock()

		errG := multierror.Group{}
		mu.RLock()
		defer mu.RUnlock()
		for cn, mcYAML := range origCfg {
			cn, mcYAML := cn, mcYAML
			c := mc.ctx.AllClusters().GetByName(cn)
			errG.Go(func() error {
				cfgMapName, err := mc.configMapName()
				if err != nil {
					return err
				}
				cfgMap, err := mc.getConfigMap(c, cfgMapName)
				if err != nil {
					return err
				}
				setMeshConfigData(cfgMap, mcYAML)
				if err := mc.updateConfigMap(c, cfgMap); err != nil {
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

func (mc *meshConfig) UpdateMeshConfigOrFail(ctx resource.Context, t test.Failer, update func(*meshconfig.MeshConfig) error, cleanupStrategy cleanup.Strategy) {
	t.Helper()
	if err := mc.UpdateMeshConfig(ctx, update, cleanupStrategy); err != nil {
		t.Fatal(err)
	}
}

func (mc *meshConfig) PatchMeshConfig(t resource.Context, patch string) error {
	return mc.UpdateMeshConfig(t, func(mc *meshconfig.MeshConfig) error {
		return protomarshal.ApplyYAML(patch, mc)
	}, cleanup.Always)
}

func (mc *meshConfig) PatchMeshConfigOrFail(ctx resource.Context, t test.Failer, patch string) {
	t.Helper()
	if err := mc.PatchMeshConfig(ctx, patch); err != nil {
		t.Fatal(err)
	}
}

func (mc *meshConfig) configMapName() (string, error) {
	i, err := Get(mc.ctx)
	if err != nil {
		return "", err
	}

	sharedCfgMapName := i.Settings().SharedMeshConfigName
	if sharedCfgMapName != "" {
		return sharedCfgMapName, nil
	}

	cmName := "istio"
	if rev := mc.revisions.Default(); rev != "default" && rev != "" {
		cmName += "-" + rev
	}
	return cmName, nil
}

func (cm *configMap) getConfigMap(c cluster.Cluster, name string) (*corev1.ConfigMap, error) {
	return c.Kube().CoreV1().ConfigMaps(cm.namespace).Get(context.TODO(), name, metav1.GetOptions{})
}

func (cm *configMap) updateConfigMap(c cluster.Cluster, cfgMap *corev1.ConfigMap) error {
	_, err := c.Kube().CoreV1().ConfigMaps(cm.namespace).Update(context.TODO(), cfgMap, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	if c.IsExternalControlPlane() {
		// Normal control plane uses ConfigMap informers to load mesh config. This is ~instant.
		// The external config uses a file mounted ConfigMap. This is super slow, but we can trigger it explicitly:
		// https://github.com/kubernetes/kubernetes/issues/30189
		pl, err := c.Kube().CoreV1().Pods(cm.namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: "app=istiod"})
		if err != nil {
			return err
		}
		for _, pod := range pl.Items {
			patchBytes := fmt.Sprintf(`{ "metadata": {"annotations": { "test.istio.io/mesh-config-hash": "%s" } } }`, hash(cfgMap.Data["mesh"]))
			_, err := c.Kube().CoreV1().Pods(cm.namespace).Patch(context.TODO(), pod.Name,
				types.MergePatchType, []byte(patchBytes), metav1.PatchOptions{FieldManager: "istio-ci"})
			if err != nil {
				return fmt.Errorf("patch %v: %v", patchBytes, err)
			}
		}
	}
	return nil
}

func hash(s string) string {
	// nolint: gosec
	// Test only code
	h := md5.New()
	_, _ = io.WriteString(h, s)
	return hex.EncodeToString(h.Sum(nil))
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

func getInjectConfigYaml(cm *corev1.ConfigMap, configKey string) (string, error) {
	if cm == nil {
		return "", fmt.Errorf("no ConfigMap found")
	}

	configYaml, exists := cm.Data[configKey]
	if !exists {
		return "", fmt.Errorf("missing ConfigMap config key %q", configKey)
	}
	return configYaml, nil
}

func injectConfigToYaml(config *inject.Config) (string, error) {
	bres, err := yaml.Marshal(config)
	return string(bres), err
}

func yamlToInjectConfig(configYaml string) (*inject.Config, error) {
	// Parse the YAML.
	c, err := inject.UnmarshalConfig([]byte(configYaml))
	if err != nil {
		return nil, err
	}
	return &c, nil
}
