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
	"context"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
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
	params := struct {
		Group   string
		Version string
	}{
		Group:   KubeSettings(t).MCSAPIGroup,
		Version: KubeSettings(t).MCSAPIVersion,
	}

	for _, kind := range []string{"serviceexport", "serviceimport"} {
		// Generate the CRD YAML
		fileName := fmt.Sprintf("mcs-%s-crd.yaml", kind)
		crdTemplate, err := os.ReadFile("../../testdata/" + fileName)
		if err != nil {
			return err
		}
		crdYAML, err := tmpl.Evaluate(string(crdTemplate), params)
		if err != nil {
			return err
		}

		// Make sure the CRD exists in each cluster.
		for _, c := range t.Clusters() {
			crdName := fmt.Sprintf("%ss.%s", kind, params.Group)
			if isCRDInstalled(c, crdName, params.Version) {
				// It's already installed on this cluster - nothing to do.
				continue
			}

			// Add/Update the CRD in this cluster...
			if t.Settings().NoCleanup {
				if err := t.ConfigKube(c).YAML("", crdYAML).Apply(apply.NoCleanup); err != nil {
					return err
				}
			} else {
				if err := t.ConfigKube(c).YAML("", crdYAML).Apply(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func isCRDInstalled(c cluster.Cluster, crdName string, version string) bool {
	crd, err := c.Ext().ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), crdName, metav1.GetOptions{})
	if err == nil {
		// Found the CRD, now check against the version.
		for _, v := range crd.Spec.Versions {
			if v.Name == version {
				// The CRD is already installed on this cluster.
				return true
			}
		}
	}
	return false
}

type EchoDeployment struct {
	Namespace namespace.Instance
	echo.Instances
}

func DeployEchosFunc(nsPrefix string, d *EchoDeployment) func(t resource.Context) error {
	return func(t resource.Context) error {
		// Create a new namespace in each cluster.
		ns, err := namespace.New(t, namespace.Config{
			Prefix: nsPrefix,
			Inject: true,
		})
		if err != nil {
			return err
		}
		d.Namespace = ns

		// Create echo instances in each cluster.
		d.Instances, err = deployment.New(t).
			WithClusters(t.Clusters()...).
			WithConfig(echo.Config{
				Service:   ServiceA,
				Namespace: ns,
				Ports:     ports.All(),
			}).
			WithConfig(echo.Config{
				Service:   ServiceB,
				Namespace: ns,
				Ports:     ports.All(),
			}).Build()
		return err
	}
}
