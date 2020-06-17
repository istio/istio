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

package bookinfo

import (
	"fmt"
	"io/ioutil"
	"path"
	"time"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

type bookInfoConfig string

const (
	// BookInfo uses "bookinfo.yaml"
	BookInfo          bookInfoConfig = "bookinfo.yaml"
	BookinfoRatingsv2 bookInfoConfig = "bookinfo-ratings-v2.yaml"
	BookinfoDB        bookInfoConfig = "bookinfo-db.yaml"
)

func deploy(ctx resource.Context, cfg Config) (undeployFunc func(), err error) {
	ns := cfg.Namespace
	if ns == nil {
		ns, err = namespace.Claim(ctx, "default", true)
		if err != nil {
			return nil, err
		}
	}

	yamlFile := path.Join(env.BookInfoKube, string(cfg.Cfg))
	by, err := ioutil.ReadFile(yamlFile)
	if err != nil {
		return nil, err
	}

	name := string(cfg.Cfg)
	scopes.Framework.Infof("=== BEGIN: Deployment %q ===", name)
	defer func() {
		if err != nil {
			err = fmt.Errorf("deployment %q failed: %v", name, err) // nolint:golint
			scopes.Framework.Errorf("Error deploying %q: %v", name, err)
			scopes.Framework.Errorf("=== FAILED: Deployment %q ===", name)
		} else {
			scopes.Framework.Infof("=== SUCCEEDED: Deployment %q ===", name)
		}
	}()

	cluster := kube.ClusterOrDefault(nil, ctx.Environment())

	d := kube2.NewYamlContentDeployment(ns.Name(), string(by), cluster.Accessor)
	if err = d.Deploy(true, retry.Timeout(time.Minute*5), retry.Delay(time.Second*5)); err != nil {
		return nil, err
	}

	return func() {
		_ = d.Delete(true, retry.Timeout(time.Minute*5), retry.Delay(time.Second*5))
	}, nil
}
