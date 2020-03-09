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

package sleep

import (
	"fmt"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/deployment"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

type SleepInstance interface {
	deployment.Instance
	Curl(uri string) (string, error)
}

type Config struct {
	Namespace namespace.Instance
	Cfg       sleepConfig
}

const (
	sleepContainerName = "sleep"
)

// Deploy returns a new instance of deployed Sleep
func Deploy(ctx resource.Context, cfg Config) (i SleepInstance, err error) {
	err = resource.UnsupportedEnvironment(ctx.Environment())

	ctx.Environment().Case(environment.Kube, func() {
		i, err = deploy(ctx, cfg)
	})

	return
}

// DeployOrFail returns a new instance of deployed Sleep or fails test
func DeployOrFail(t test.Failer, ctx resource.Context, cfg Config) SleepInstance {
	t.Helper()

	i, err := Deploy(ctx, cfg)
	if err != nil {
		t.Fatalf("sleep.DeployOrFail: %v", err)
	}

	return i
}

// Curl makes a GET request to the given url for the sleep deployment and returns
// the HTTP response code
func (s *sleepComponent) Curl(url string) (string, error) {
	pods, err := s.Env().GetPods(s.Namespace().Name(), "app=sleep")
	if err != nil {
		return "", fmt.Errorf("unable to find sleep pod: %v", err)
	}
	podNs, podName := pods[0].Namespace, pods[0].Name

	// Exec onto the pod and make a curl request for the given curl
	command := fmt.Sprintf("curl -w %%{http_code} %s", url)
	return s.Env().Accessor.Exec(podNs, podName, sleepContainerName, command)
}
