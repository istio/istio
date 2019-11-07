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

package bookinfo

import (
	"io/ioutil"
	"path"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

type bookInfoConfig string

const (
	// BookInfo uses "bookinfo.yaml"
	BookInfo          bookInfoConfig = "bookinfo.yaml"
	BookinfoRatingsv2 bookInfoConfig = "bookinfo-ratings-v2.yaml"
	BookinfoDb        bookInfoConfig = "bookinfo-db.yaml"
)

func deploy(ctx resource.Context, cfg Config) (i deployment.Instance, err error) {
	ns := cfg.Namespace
	if ns == nil {
		ns, err = namespace.Claim(ctx, "default")
		if err != nil {
			return nil, err
		}
	}

	yamlFile := path.Join(env.BookInfoKube, string(cfg.Cfg))
	by, err := ioutil.ReadFile(yamlFile)
	if err != nil {
		return nil, err
	}

	depcfg := deployment.Config{
		Name:      string(cfg.Cfg),
		Namespace: ns,
		Yaml:      string(by),
	}

	return deployment.New(ctx, depcfg)
}
