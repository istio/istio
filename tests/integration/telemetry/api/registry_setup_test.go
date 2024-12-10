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

package api

import (
	"encoding/base64"
	"fmt"

	"istio.io/istio/pkg/test/framework/components/registryredirector"
	"istio.io/istio/pkg/test/framework/resource"
)

var registry registryredirector.Instance

const (
	// The password is a token that taken from the created service account
	registryUser = "user"
)

func testRegistrySetup(ctx resource.Context) (err error) {
	var targetRegistry, scheme string

	if ctx.Settings().OpenShift {
		targetRegistry = "image-registry.openshift-image-registry.svc:5000"
		scheme = "https"
	} else {
		targetRegistry = "kind-registry:5000"
		scheme = "http"
	}

	registry, err = registryredirector.New(ctx, registryredirector.Config{
		Cluster:        ctx.AllClusters().Default(),
		TargetRegistry: targetRegistry,
		Scheme:         scheme,
	})
	if err != nil {
		return
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
