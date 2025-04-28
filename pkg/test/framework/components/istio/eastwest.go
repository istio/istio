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
	"os"
	"os/exec"
	"path"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
)

const (
	eastWestIngressIstioNameLabel = "eastwestgateway"
	eastWestIngressIstioLabel     = "istio=" + eastWestIngressIstioNameLabel
	eastWestIngressServiceName    = "istio-" + eastWestIngressIstioNameLabel
)

var (
	mcSamples              = path.Join(env.IstioSrc, "samples", "multicluster")
	exposeIstiodGateway    = path.Join(mcSamples, "expose-istiod.yaml")
	exposeIstiodGatewayRev = path.Join(mcSamples, "expose-istiod-rev.yaml.tmpl")
	exposeServicesGateway  = path.Join(mcSamples, "expose-services.yaml")
	genGatewayScript       = path.Join(mcSamples, "gen-eastwest-gateway.sh")
)

// deployEastWestGateway will create a separate gateway deployment for cross-cluster discovery or cross-network services.
func (i *istioImpl) deployEastWestGateway(cluster cluster.Cluster, revision string, customSettings string) error {
	// generate istio operator yaml
	args := []string{
		"--cluster", cluster.Name(),
		"--network", cluster.NetworkName(),
		"--revision", revision,
		"--mesh", meshID,
	}
	if !i.env.IsMultiCluster() {
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

	// Install the gateway
	s := i.ctx.Settings()
	var inFileNames []string
	inFileNames = append(inFileNames, iopFile)
	if customSettings != "" {
		inFileNames = append(inFileNames, customSettings)
	}

	setArgs := []string{
		"hub=" + s.Image.Hub,
		"tag=" + s.Image.Tag,
		"values.global.imagePullPolicy=" + s.Image.PullPolicy,
		"values.gateways.istio-ingressgateway.autoscaleEnabled=false",
	}

	if i.cfg.SystemNamespace != "istio-system" {
		setArgs = append(setArgs, "values.global.istioNamespace="+i.cfg.SystemNamespace)
	}

	for k, v := range i.cfg.OperatorOptions {
		setArgs = append(setArgs, fmt.Sprintf("%s=%s", k, v))
	}

	if err := i.installer.Install(cluster, installArgs{
		ComponentName: "eastwestgateway",
		Revision:      revision,
		Files:         inFileNames,
		Set:           setArgs,
	}); err != nil {
		return err
	}

	// wait for a ready pod
	if err := retry.UntilSuccess(func() error {
		pods, err := cluster.Kube().CoreV1().Pods(i.cfg.SystemNamespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: eastWestIngressIstioLabel,
		})
		if err != nil {
			return err
		}
		for _, p := range pods.Items {
			if p.Status.Phase == corev1.PodRunning {
				return nil
			}
		}
		return fmt.Errorf("no ready pods for " + eastWestIngressIstioLabel)
	}, componentDeployTimeout, componentDeployDelay); err != nil {
		return fmt.Errorf("failed waiting for %s to become ready: %v", eastWestIngressServiceName, err)
	}

	return nil
}

func (i *istioImpl) exposeUserServices(cluster cluster.Cluster) error {
	scopes.Framework.Infof("Exposing services via eastwestgateway in %v", cluster.Name())
	return cluster.ApplyYAMLFiles(i.cfg.SystemNamespace, exposeServicesGateway)
}

func (i *istioImpl) applyIstiodGateway(cluster cluster.Cluster, revision string) error {
	scopes.Framework.Infof("Exposing istiod via eastwestgateway in %v", cluster.Name())
	if revision == "" {
		return cluster.ApplyYAMLFiles(i.cfg.SystemNamespace, exposeIstiodGateway)
	}
	gwTmpl, err := os.ReadFile(exposeIstiodGatewayRev)
	if err != nil {
		return fmt.Errorf("failed loading template %s: %v", exposeIstiodGatewayRev, err)
	}
	out, err := tmpl.Evaluate(string(gwTmpl), map[string]string{"Revision": revision})
	if err != nil {
		return fmt.Errorf("failed running template %s: %v", exposeIstiodGatewayRev, err)
	}
	return i.ctx.ConfigKube(cluster).YAML(i.cfg.SystemNamespace, out).Apply()
}
