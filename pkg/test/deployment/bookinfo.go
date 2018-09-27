//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package deployment

import (
	"path"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/scopes"
	"istio.io/istio/pkg/test/kube"
)

type bookInfoVariant string

const (
	// VariantBookInfo uses the default BookInfo variant in "bookinfo.yaml"
	VariantBookInfo bookInfoVariant = "bookinfo.yaml"
)

// NewBookInfo deploys BookInfo.
func NewBookInfo(s *Settings, variant bookInfoVariant, a *kube.Accessor) (instance *Instance, err error) {
	scopes.CI.Info("=== BEGIN: Deploy BookInfo (via Yaml File) ===")
	defer func() {
		if err != nil {
			instance = nil
			scopes.CI.Infof("=== FAILED: Deploy BookInfo ===")
		} else {
			scopes.CI.Infof("=== SUCCEEDED: Deploy BookInfo ===")
		}
	}()

	yamlFile := path.Join(env.BookInfoKube, string(variant))
	return newYamlDeployment(s, a, yamlFile)
}
