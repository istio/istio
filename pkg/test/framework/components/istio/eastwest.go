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

package istio

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/operator/cmd/mesh"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
)

const (
	eastWestIngressIstioLabel  = "eastwestgateway"
	eastWestIngressServiceName = "istio-" + eastWestIngressIstioLabel
)

var (
	mcSamples              = path.Join(env.IstioSrc, "samples", "multicluster")
	exposeIstiodGateway    = path.Join(mcSamples, "expose-istiod.yaml")
	exposeIstiodGatewayRev = path.Join(mcSamples, "expose-istiod-rev.yaml.tmpl")
	exposeServicesGateway  = path.Join(mcSamples, "expose-services.yaml")
	genGatewayScript       = path.Join(mcSamples, "gen-eastwest-gateway.sh")
)

// deployEastWestGateway will create a separate gateway deployment for cross-cluster discovery or cross-network services.
func (i *operatorComponent) deployEastWestGateway(cluster cluster.Cluster, revision string) error {
	// generate istio operator yaml
	args := []string{
		"--cluster", cluster.Name(),
		"--network", cluster.NetworkName(),
		"--revision", revision,
		"--mesh", meshID,
	}
	if !i.environment.IsMulticluster() {
		args = []string{"--single-cluster"}
	}
	cmd := exec.Command(genGatewayScript, args...)
	gwIOP, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed generating eastwestgateway operator yaml: %v: %v", err, string(gwIOP))
	}
	iopFile := path.Join(i.workDir, fmt.Sprintf("eastwest-%s.yaml", cluster.Name()))
	if err := os.WriteFile(iopFile, gwIOP, os.ModePerm); err != nil {
		return err
	}

	// Save the manifest generate output so we can later cleanup
	s := i.ctx.Settings()
	manifestGenArgs := &mesh.ManifestGenerateArgs{
		InFilenames: []string{iopFile},
		Set: []string{
			"hub=" + s.Image.Hub,
			"tag=" + s.Image.Tag,
			"values.global.imagePullPolicy=" + s.Image.PullPolicy,
			"values.gateways.istio-ingressgateway.autoscaleEnabled=false",
		},
		ManifestsPath: filepath.Join(env.IstioSrc, "manifests"),
		Revision:      revision,
	}

	var stdOut, stdErr bytes.Buffer
	if err := mesh.ManifestGenerate(&mesh.RootArgs{}, manifestGenArgs, cmdLogOptions(), cmdLogger(&stdOut, &stdErr)); err != nil {
		return err
	}
	i.saveManifestForCleanup(cluster.Name(), stdOut.String())

	kubeConfigFile, err := kubeConfigFileForCluster(cluster)
	if err != nil {
		return err
	}

	installArgs := &mesh.InstallArgs{
		InFilenames:    []string{iopFile},
		KubeConfigPath: kubeConfigFile,
		Set:            manifestGenArgs.Set,
		ManifestsPath:  manifestGenArgs.ManifestsPath,
		Revision:       manifestGenArgs.Revision,
	}

	scopes.Framework.Infof("Deploying eastwestgateway in %s: %v", cluster.Name(), installArgs)
	err = install(i, installArgs, cluster.Name())
	if err != nil {
		scopes.Framework.Error(err)
		return fmt.Errorf("failed installing eastwestgateway via IstioOperator: %v", err)
	}

	// wait for a ready pod
	if err := retry.UntilSuccess(func() error {
		pods, err := cluster.Kube().CoreV1().Pods(i.settings.SystemNamespace).List(context.TODO(), v1.ListOptions{
			LabelSelector: "istio=" + eastWestIngressIstioLabel,
		})
		if err != nil {
			return err
		}
		for _, p := range pods.Items {
			if p.Status.Phase == corev1.PodRunning {
				return nil
			}
		}
		return fmt.Errorf("no ready pods for istio=" + eastWestIngressIstioLabel)
	}, componentDeployTimeout, componentDeployDelay); err != nil {
		return fmt.Errorf("failed waiting for %s to become ready: %v", eastWestIngressServiceName, err)
	}

	return nil
}

func (i *operatorComponent) exposeUserServices(cluster cluster.Cluster) error {
	scopes.Framework.Infof("Exposing services via eastwestgateway in %v", cluster.Name())
	return cluster.ApplyYAMLFiles(i.settings.SystemNamespace, exposeServicesGateway)
}

func (i *operatorComponent) applyIstiodGateway(cluster cluster.Cluster, revision string) error {
	scopes.Framework.Infof("Exposing istiod via eastwestgateway in %v", cluster.Name())
	if revision == "" {
		return cluster.ApplyYAMLFiles(i.settings.SystemNamespace, exposeIstiodGateway)
	}
	gwTmpl, err := os.ReadFile(exposeIstiodGatewayRev)
	if err != nil {
		return fmt.Errorf("failed loading template %s: %v", exposeIstiodGatewayRev, err)
	}
	out, err := tmpl.Evaluate(string(gwTmpl), map[string]string{"Revision": revision})
	if err != nil {
		return fmt.Errorf("failed running template %s: %v", exposeIstiodGatewayRev, err)
	}
	return i.ctx.ConfigKube(cluster).YAML(i.settings.SystemNamespace, out).Apply()
}
