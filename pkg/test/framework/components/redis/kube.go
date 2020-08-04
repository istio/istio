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

package redis

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"

	environ "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/framework/resource"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/tmpl"
)

const (
	redisNamespace = "istio-redis"
)

var (
	_ Instance  = &kubeComponent{}
	_ io.Closer = &kubeComponent{}
)

type kubeComponent struct {
	id      resource.ID
	ns      namespace.Instance
	cluster resource.Cluster
}

func newKube(ctx resource.Context, cfg Config) (Instance, error) {
	c := &kubeComponent{
		cluster: ctx.Clusters().GetOrDefault(cfg.Cluster),
	}
	c.id = ctx.TrackResource(c)
	var err error
	scopes.Framework.Info("=== BEGIN: Deploy Redis ===")
	defer func() {
		if err != nil {
			err = fmt.Errorf("redis deployment failed: %v", err) // nolint:golint
			scopes.Framework.Infof("=== FAILED: Deploy Redis ===")
			_ = c.Close()
		} else {
			scopes.Framework.Info("=== SUCCEEDED: Deploy Redis ===")
		}
	}()

	c.ns, err = namespace.New(ctx, namespace.Config{
		Prefix: redisNamespace,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create %s Namespace for Redis install; err:%v", redisNamespace, err)
	}

	if err := environ.CheckFileExists(environ.ServiceAccountFilePath); err != nil {
		return nil, fmt.Errorf("failed to file service account file %s, err: %v", environ.ServiceAccountFilePath, err)
	}

	if err := c.cluster.ApplyYAMLFiles("kube-system", environ.ServiceAccountFilePath); err != nil {
		return nil, fmt.Errorf("failed to apply %s, err: %v", environ.ServiceAccountFilePath, err)
	}

	// apply redis YAML
	s, err := image.SettingsFromCommandLine()
	if err != nil {
		return nil, err
	}

	templateBytes, err := ioutil.ReadFile(environ.RedisInstallFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s, err: %v", environ.RedisInstallFilePath, err)
	}

	yamlContent, err := tmpl.Evaluate(string(templateBytes), map[string]interface{}{
		"BitnamiHub":      s.BitnamiHub,
		"ImagePullPolicy": s.PullPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to render %s, err: %v", environ.RedisInstallFilePath, err)
	}

	if err := ctx.Config(c.cluster).ApplyYAML(c.ns.Name(), yamlContent); err != nil {
		return nil, fmt.Errorf("failed to apply rendered %s, err: %v", environ.RedisInstallFilePath, err)
	}

	fetchFn := kube2.NewPodFetch(c.cluster, c.ns.Name(), "app=redis")
	if _, err := kube2.WaitUntilPodsAreReady(fetchFn); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

// Close implements io.Closer.
func (c *kubeComponent) Close() error {
	_ = c.cluster.CoreV1().Namespaces().Delete(context.TODO(), redisNamespace, kube2.DeleteOptionsForeground())
	_ = kube2.WaitForNamespaceDeletion(c.cluster, redisNamespace)
	return nil
}

func (c *kubeComponent) GetRedisNamespace() string {
	return c.ns.Name()
}
