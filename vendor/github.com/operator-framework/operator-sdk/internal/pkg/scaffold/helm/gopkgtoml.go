// Copyright 2019 The Operator-SDK Authors
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

package helm

import (
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
)

// GopkgToml - the Gopkg.toml file for a hybrid operator
type GopkgToml struct {
	input.Input
}

func (s *GopkgToml) GetInput() (input.Input, error) {
	if s.Path == "" {
		s.Path = scaffold.GopkgTomlFile
	}
	s.TemplateBody = gopkgTomlTmpl
	return s.Input, nil
}

const gopkgTomlTmpl = `[[constraint]]
  name = "github.com/operator-framework/operator-sdk"
  # The version rule is used for a specific release and the master branch for in between releases.
  # branch = "master" #osdk_branch_annotation
  version = "=v0.7.0" #osdk_version_annotation

[[override]]
  name = "k8s.io/api"
  version = "kubernetes-1.13.1"

[[override]]
  name = "k8s.io/apimachinery"
  version = "kubernetes-1.13.1"

[[override]]
  name = "k8s.io/apiextensions-apiserver"
  version = "kubernetes-1.13.1"

[[override]]
  name = "k8s.io/apiserver"
  version = "kubernetes-1.13.1"

[[override]]
  name = "k8s.io/client-go"
  version = "kubernetes-1.13.1"

[[override]]
  name = "k8s.io/cli-runtime"
  version = "kubernetes-1.13.1"

# We need overrides for the following imports because dep can't resolve them
# correctly. The easiest way to get this right is to use the versions that
# k8s.io/helm uses. See https://github.com/helm/helm/blob/v2.13.1/glide.lock
[[override]]
  name = "k8s.io/kubernetes"
  revision = "c6d339953bd4fd8c021a6b5fb46d7952b30be9f9"

[[override]]
name = "github.com/russross/blackfriday"
revision = "300106c228d52c8941d4b3de6054a6062a86dda3"

[[override]]
name = "github.com/docker/distribution"
revision = "edc3ab29cdff8694dd6feb85cfeb4b5f1b38ed9c"

[[override]]
name = "github.com/docker/docker"
revision = "a9fbbdc8dd8794b20af358382ab780559bca589d"

[prune]
  go-tests = true
  unused-packages = true
`
