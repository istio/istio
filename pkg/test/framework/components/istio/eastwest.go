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
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	mcSamples             = path.Join(env.IstioSrc, "samples", "multicluster")
	exposeIstiodGateway   = path.Join(mcSamples, "expose-istiod.yaml")
	exposeServicesGateway = path.Join(mcSamples, "expose-services.yaml")
	genGatewayScript      = path.Join(mcSamples, "gen-eastwest-gateway.sh")
)

// deployEastWestGateway will create a separate gateway deployment for cross-cluster discovery or cross-network services.
func (i *operatorComponent) deployEastWestGateway(cluster cluster.Cluster, revision string) error {
	imgSettings, err := image.SettingsFromCommandLine()
	if err != nil {
		return err
	}

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
		return fmt.Errorf("failed generating eastwestgateway operator yaml: %v", err)
	}
	iopFile := path.Join(i.workDir, fmt.Sprintf("eastwest-%s.yaml", cluster.Name()))
	if err := ioutil.WriteFile(iopFile, gwIOP, os.ModePerm); err != nil {
		return err
	}

	// use operator yaml to generate k8s resources
	istioCtl, err := istioctl.New(i.ctx, istioctl.Config{Cluster: cluster})
	if err != nil {
		return err
	}

	installSettings := []string{
		"--istioNamespace", i.settings.SystemNamespace,
		"--manifests", filepath.Join(env.IstioSrc, "manifests"),
		"--set", "hub=" + imgSettings.Hub,
		"--set", "tag=" + imgSettings.Tag,
		"--set", "values.global.imagePullPolicy=" + imgSettings.PullPolicy,
		"-f", iopFile,
	}
	if revision != "" {
		installSettings = append(installSettings, "--revision", revision)
	}

	// Save the manifest generate output so we can later cleanup
	genCmd := []string{"manifest", "generate"}
	genCmd = append(genCmd, installSettings...)
	out, _, err := istioCtl.Invoke(genCmd)
	if err != nil {
		return err
	}
	i.saveManifestForCleanup(cluster.Name(), out)

	scopes.Framework.Infof("Deploying eastwestgateway in %s: %v", cluster.Name(), installSettings)
	err = install(i, installSettings, istioCtl, cluster.Name())
	if err != nil {
		scopes.Framework.Error(err)
		return fmt.Errorf("failed installing eastwestgateway via IstioOperator: %v", err)
	}

	// wait for a ready pod
	if err := retry.UntilSuccess(func() error {
		pods, err := cluster.CoreV1().Pods(i.settings.SystemNamespace).List(context.TODO(), v1.ListOptions{
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

func (i *operatorComponent) applyIstiodGateway(cluster cluster.Cluster) error {
	scopes.Framework.Infof("Exposing istiod via eastwestgateway in %v", cluster.Name())
	return cluster.ApplyYAMLFiles(i.settings.SystemNamespace, exposeIstiodGateway)
}
