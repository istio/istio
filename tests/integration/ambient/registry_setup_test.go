//go:build integ
// +build integ

// Copyright Istio Authors. All Rights Reserved.
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

package ambient

import (
	"encoding/base64"
	"fmt"

	"istio.io/istio/pkg/test/framework/components/registryredirector"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
)

var registry registryredirector.Instance

const (
	// Same user name and password as specified at pkg/test/fakes/imageregistry
	registryUser   = "user"
	registryPasswd = "passwd"
)

func testRegistrySetup(ctx resource.Context) (err error) {
	var config registryredirector.Config

	isKind := ctx.Clusters().IsKindCluster()

	// By default, for any platform, the test will pull the test image from public "gcr.io" registry.
	// For kind clusters, it will pull images from the "kind-registry" local registry container. In IPv6-only clusters
	// this is not supported as CoreDNS will attempt to "talk" to the upstream DNS over docker/IPv4 for resolution.
	// For context: https://github.com/kubernetes-sigs/kind/issues/3114
	// and https://github.com/istio/istio/pull/51778
	if isKind {
		config = registryredirector.Config{
			Cluster:        ctx.AllClusters().Default(),
			TargetRegistry: "kind-registry:5000",
			Scheme:         "http",
		}
	}

	registry, err = registryredirector.New(ctx, config)
	if err != nil {
		return
	}

	args := map[string]any{
		"DockerConfigJson": base64.StdEncoding.EncodeToString(
			[]byte(createDockerCredential(registryUser, registryPasswd, registry.Address()))),
	}
	if err := ctx.ConfigIstio().EvalFile(apps.Namespace.Name(), args, "testdata/registry-secret.yaml").
		Apply(apply.CleanupConditionally); err != nil {
		return err
	}
	return nil
}

func createDockerCredential(user, passwd, registry string) string {
	credentials := `{
	"auths":{
		"%v":{
			"username": "%v",
			"password": "%v",
			"email": "test@example.com",
			"auth": "%v"
		}
	}
}`
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + passwd))
	return fmt.Sprintf(credentials, registry, user, passwd, auth)
}
