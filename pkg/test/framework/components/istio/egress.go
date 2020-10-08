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
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	egressIstioLabel   = "egressgateway"
	egressServiceName  = "istio-" + egressIstioLabel
	egressOperatorYAML = `
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  profile: empty
  components:
    egressGateways:
      - name: istio-egressgateway
        enabled: true
`
)

// deployEgress creates and applies a separate deployment for istio-egressgateway.
func (i *operatorComponent) deployEgress(cluster resource.Cluster) error {
	if strings.Contains(i.settings.ControlPlaneValues, egressServiceName) {
		scopes.Framework.Infof("Skipping egress install, since % specified by ControlPlaneValues",
			egressServiceName)
		return nil
	}
	imgSettings, err := image.SettingsFromCommandLine()
	if err != nil {
		return err
	}

	// Create the yaml for this cluster.
	iopFile := filepath.Join(i.workDir, fmt.Sprintf("egress-%s.yaml", cluster.Name()))
	if err := ioutil.WriteFile(iopFile, []byte(egressOperatorYAML), os.ModePerm); err != nil {
		return err
	}

	installSettings := []string{
		"manifest", "generate",
		"--istioNamespace", i.settings.SystemNamespace,
		"--manifests", filepath.Join(env.IstioSrc, "manifests"),
		"--set", "hub=" + imgSettings.Hub,
		"--set", "tag=" + imgSettings.Tag,
		"--set", "values.global.imagePullPolicy=" + imgSettings.PullPolicy,
		"-f", iopFile,
	}

	// use operator yaml to generate k8s resources
	istioCtl, err := istioctl.New(i.ctx, istioctl.Config{Cluster: cluster})
	if err != nil {
		return err
	}

	scopes.Framework.Infof("Deploying egress in %s: %v", cluster.Name(), installSettings)
	gwYaml, stderr, err := istioCtl.Invoke(installSettings)
	if err != nil {
		scopes.Framework.Error(gwYaml)
		scopes.Framework.Error(stderr)
		scopes.Framework.Error(err)
		return fmt.Errorf("failed installing eastwestgateway via IstioOperator: %v", err)
	}

	// apply k8s resources
	if err := i.ctx.Config(cluster).ApplyYAML(i.settings.SystemNamespace, gwYaml); err != nil {
		return err
	}

	// cleanup using operator yaml later
	i.saveManifestForCleanup(cluster.Name(), gwYaml)

	// wait for a ready pod
	if err := retry.UntilSuccess(func() error {
		pods, err := cluster.CoreV1().Pods(i.settings.SystemNamespace).List(context.TODO(), v1.ListOptions{
			LabelSelector: "istio=" + egressIstioLabel,
		})
		if err != nil {
			return err
		}
		for _, p := range pods.Items {
			if p.Status.Phase == corev1.PodRunning {
				return nil
			}
		}
		return fmt.Errorf("no ready pods for istio=" + egressIstioLabel)
	}, componentDeployTimeout, componentDeployDelay); err != nil {
		return fmt.Errorf("failed waiting for %s to become ready: %v", egressServiceName, err)
	}

	return nil
}
