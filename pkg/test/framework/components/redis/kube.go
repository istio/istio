// Copyright 2019 Istio Authors
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
	"fmt"
	"io"

	environ "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/tests/util"
)

const (
	redisInstallDir  = "stable/redis"
	redisInstallName = "redis-release"
	redisNamespace   = "istio-redis"
)

var (
	_ Instance  = &kubeComponent{}
	_ io.Closer = &kubeComponent{}
)

type kubeComponent struct {
	id  resource.ID
	env *kube.Environment
	ns  namespace.Instance
}

func newKube(ctx resource.Context) (Instance, error) {
	env := ctx.Environment().(*kube.Environment)
	c := &kubeComponent{
		env: env,
	}
	c.id = ctx.TrackResource(c)
	var err error
	scopes.CI.Info("=== BEGIN: Deploy Redis ===")
	defer func() {
		if err != nil {
			err = fmt.Errorf("redis deployment failed: %v", err) // nolint:golint
			scopes.CI.Infof("=== FAILED: Deploy Redis ===")
			_ = c.Close()
		} else {
			scopes.CI.Info("=== SUCCEEDED: Deploy Redis ===")
		}
	}()

	c.ns, err = namespace.New(ctx, redisNamespace, false)
	if err != nil {
		return nil, fmt.Errorf("could not create %s Namespace for Redis install; err:%v", redisNamespace, err)
	}

	if err := environ.CheckFileExists(environ.ServiceAccountFilePath); err != nil {
		return nil, fmt.Errorf("failed to file service account file %s, err: %v", environ.ServiceAccountFilePath, err)
	}

	if err := env.Apply("kube-system", environ.ServiceAccountFilePath); err != nil {
		return nil, fmt.Errorf("failed to apply %s, err: %v", environ.ServiceAccountFilePath, err)
	}

	// deploy tiller, only if not already installed.
	if err := util.HelmTillerRunning(); err != nil {
		if err := util.HelmInit("tiller"); err != nil {
			return nil, fmt.Errorf("failed to init helm tiller, err: %v", err)
		}
		if err := util.HelmTillerRunning(); err != nil {
			return nil, fmt.Errorf("tiller failed to start, err: %v", err)
		}
	}

	setValue := "--set usePassword=false,persistence.enabled=false"
	if err := util.HelmInstall(redisInstallDir, redisInstallName, "", c.ns.Name(), setValue); err != nil {
		return nil, fmt.Errorf("helm install %s failed, setValue=%s, err: %v", redisInstallDir, setValue, err)
	}

	fetchFn := c.env.NewPodFetch(c.ns.Name(), "app=redis")
	if _, err := c.env.WaitUntilPodsAreReady(fetchFn); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

// Close implements io.Closer.
func (c *kubeComponent) Close() error {
	scopes.CI.Infof("Deleting Redis Install")
	_ = util.HelmDelete(redisInstallName)
	_ = c.env.WaitForNamespaceDeletion(redisNamespace)
	return nil
}

func (c *kubeComponent) GetRedisNamespace() string {
	return c.ns.Name()
}
