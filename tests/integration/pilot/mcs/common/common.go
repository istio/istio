//go:build integ
// +build integ

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

package common

import (
	"os"

	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/tmpl"
)

const (
	ServiceA = "svc-a"
	ServiceB = "svc-b"
)

func IsMCSControllerEnabled(t resource.Context) bool {
	return KubeSettings(t).MCSControllerEnabled
}

func KubeSettings(t resource.Context) *kube.Settings {
	return t.Environment().(*kube.Environment).Settings()
}

func InstallMCSCRDs(t resource.Context) error {
	if !IsMCSControllerEnabled(t) {
		params := struct {
			Group   string
			Version string
		}{
			Group:   KubeSettings(t).MCSAPIGroup,
			Version: KubeSettings(t).MCSAPIVersion,
		}
		for _, f := range []string{"mcs-serviceexport-crd.yaml", "mcs-serviceimport-crd.yaml"} {
			crdTemplate, err := os.ReadFile("../../testdata/" + f)
			if err != nil {
				return err
			}
			crd, err := tmpl.Evaluate(string(crdTemplate), params)
			if err != nil {
				return err
			}
			if t.Settings().NoCleanup {
				if err := t.ConfigKube().ApplyYAMLNoCleanup("", crd); err != nil {
					return err
				}
			} else {
				if err := t.ConfigKube().ApplyYAML("", crd); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type EchoDeployment struct {
	Namespace string
	echo.Instances
}

func DeployEchosFunc(nsPrefix string, deployment *EchoDeployment) func(t resource.Context) error {
	return func(t resource.Context) error {
		// Create a new namespace in each cluster.
		ns, err := namespace.New(t, namespace.Config{
			Prefix: nsPrefix,
			Inject: true,
		})
		if err != nil {
			return err
		}
		deployment.Namespace = ns.Name()

		// Create echo instances in each cluster.
		deployment.Instances, err = echoboot.NewBuilder(t).
			WithClusters(t.Clusters()...).
			WithConfig(echo.Config{
				Service:   ServiceA,
				Namespace: ns,
				Ports:     common.EchoPorts,
			}).
			WithConfig(echo.Config{
				Service:   ServiceB,
				Namespace: ns,
				Ports:     common.EchoPorts,
			}).Build()
		return err
	}
}
