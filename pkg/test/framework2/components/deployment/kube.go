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

	"istio.io/istio/pkg/test/scopes"

	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/framework2/components/environment/kube"
	"istio.io/istio/pkg/test/framework2/core"
)

type kubeComponent struct {
	id         core.ResourceID
	cfg        Config
	env        *kube.Environment
	deployment *deployment.Instance
}

var _ Instance = &kubeComponent{}
var _ io.Closer = &kubeComponent{}

func newKube(ctx core.Context, cfg Config) (Instance, error) {
	e := ctx.Environment().(*kube.Environment)

	ns := ""
	if cfg.Namespace != nil {
		ns = cfg.Namespace.Name()
	}

	i := &kubeComponent{
		cfg:        cfg,
		deployment: deployment.NewYamlDeployment(ns, cfg.Yaml),
		env:        e,
	}
	i.id = ctx.TrackResource(i)

	scopes.CI.Infof("=== BEGIN: Deployment %q ===", cfg.Name)
	var err error
	defer func() {
		if err != nil {
			err = fmt.Errorf("deployment %q failed: %v", cfg.Name, err) // nolint:golint
			scopes.CI.Errorf("=== FAILED: Deployment %q ===", cfg.Name)
		} else {
			scopes.CI.Errorf("=== SUCCEEDED: Deployment %q ===", cfg.Name)
		}
	}()

	if err = i.deployment.Deploy(e.Accessor, true, cfg.RetryOptions...); err != nil {
		return nil, err
	}

	return i, nil
}

func (c *kubeComponent) ID() core.ResourceID {
	return c.id
}

func (c *kubeComponent) Name() string {
	return c.cfg.Name
}

func (c *kubeComponent) Namespace() core.Namespace {
	return c.cfg.Namespace
}

func (c *kubeComponent) Close() (err error) {
	if c.deployment != nil {
		err = c.deployment.Delete(c.env.Accessor, true, c.cfg.RetryOptions...)
		c.deployment = nil
	}

	return
}
