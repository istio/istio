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

package wasm

import (
	"encoding/base64"
	"fmt"

	"istio.io/istio/pkg/test/framework/components/registryredirector"
	"istio.io/istio/pkg/test/framework/resource"
	common "istio.io/istio/tests/integration/telemetry/stats/prometheus"
)

var registry registryredirector.Instance

const (
	// Same user name and password as specified at pkg/test/fakes/imageregistry
	registryUser   = "user"
	registryPasswd = "passwd"
)

func testSetup(ctx resource.Context) (err error) {
	registry, err = registryredirector.New(ctx, registryredirector.Config{
		Cluster: ctx.AllClusters().Default(),
	})
	if err != nil {
		return
	}

	args := map[string]interface{}{
		"DockerConfigJson": base64.StdEncoding.EncodeToString(
			[]byte(createDockerCredential(registryUser, registryPasswd, registry.Address()))),
	}
	if err := ctx.ConfigIstio().EvalFile(common.GetAppNamespace().Name(), args, "testdata/registry-secret.yaml").
		Apply(); err != nil {
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
