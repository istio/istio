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

package deployment

import (
	"fmt"
	"io"
	"time"

	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

type kubeComponent struct {
	id         resource.ID
	cfg        Config
	env        *kube.Environment
	deployment *deployment.Instance
}

var _ Instance = &kubeComponent{}
var _ io.Closer = &kubeComponent{}

func newKube(ctx resource.Context, cfg Config) (Instance, error) {
	e := ctx.Environment().(*kube.Environment)

	ns := ""
	if cfg.Namespace != nil {
		ns = cfg.Namespace.Name()
	}

	i := &kubeComponent{
		cfg:        cfg,
		deployment: deployment.NewYamlContentDeployment(ns, cfg.Yaml),
		env:        e,
	}
	i.id = ctx.TrackResource(i)

	scopes.CI.Infof("=== BEGIN: Deployment %q ===", cfg.Name)
	var err error
	defer func() {
		if err != nil {
			err = fmt.Errorf("deployment %q failed: %v", cfg.Name, err) // nolint:golint
			scopes.Framework.Errorf("Error deploying %q: %v", cfg.Name, err)
			scopes.CI.Errorf("=== FAILED: Deployment %q ===", cfg.Name)
		} else {
			scopes.CI.Infof("=== SUCCEEDED: Deployment %q ===", cfg.Name)
		}
	}()

	if err = i.deployment.Deploy(e.Accessor, true, retry.Timeout(time.Minute*5), retry.Delay(time.Second*5)); err != nil {
		return nil, err
	}

	return i, nil
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

func (c *kubeComponent) Name() string {
	return c.cfg.Name
}

func (c *kubeComponent) Namespace() namespace.Instance {
	return c.cfg.Namespace
}

func (c *kubeComponent) Close() (err error) {
	if c.deployment != nil {
		err = c.deployment.Delete(c.env.Accessor, true, retry.Timeout(time.Minute*5), retry.Delay(time.Second*5))
		c.deployment = nil
	}

	return
}
