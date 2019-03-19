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

package istio

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"

	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

type kubeComponent struct {
	id          resource.ID
	settings    Config
	ctx         resource.Context
	environment *kube.Environment
	deployment  *deployment.Instance
}

var _ io.Closer = &kubeComponent{}
var _ Instance = &kubeComponent{}
var _ resource.Dumper = &kubeComponent{}

func deploy(ctx resource.Context, env *kube.Environment, cfg Config) (Instance, error) {
	scopes.CI.Infof("=== Istio Component Config ===")
	scopes.CI.Infof("\n%s", cfg.String())
	scopes.CI.Infof("================================")

	i := &kubeComponent{
		environment: env,
		settings:    cfg,
		ctx:         ctx,
	}
	i.id = ctx.TrackResource(i)

	if !cfg.DeployIstio {
		scopes.Framework.Info("skipping deployment due to Config")
		return i, nil
	}

	helmDir, err := ctx.CreateTmpDirectory("istio-deployment-yaml-split")
	if err != nil {
		return nil, err
	}

	generatedYaml, err := generateIstioYaml(helmDir, cfg)
	if err != nil {
		return nil, err
	}

	// Write out istio.yaml for debugging purposes.
	dir, err := ctx.CreateTmpDirectory("istio-deployment-yaml")
	if err != nil {
		scopes.Framework.Errorf("Unable to create tmp directory to write out istio.yaml: %v", err)
	} else {
		p := path.Join(dir, "istio.yaml")
		if err = ioutil.WriteFile(p, []byte(generatedYaml), os.ModePerm); err != nil {
			scopes.Framework.Errorf("error writing istio.yaml: %v", err)
		} else {
			scopes.Framework.Debugf("Wrote out istio.yaml at: %s", p)
			scopes.CI.Infof("Wrote out istio.yaml at: %s", p)
		}
	}
	// split installation & configuration into two distinct steps int
	installYaml, configureYaml := splitIstioYaml(generatedYaml)

	installYamlFilePath := path.Join(helmDir, "istio-install.yaml")
	if err = ioutil.WriteFile(installYamlFilePath, []byte(installYaml), os.ModePerm); err != nil {
		return nil, fmt.Errorf("unable to write helm generated yaml: %v", err)
	}

	configureYamlFilePath := path.Join(helmDir, "istio-configure.yaml")
	if err = ioutil.WriteFile(configureYamlFilePath, []byte(configureYaml), os.ModePerm); err != nil {
		return nil, fmt.Errorf("unable to write helm generated yaml: %v", err)
	}

	scopes.CI.Infof("Created Helm-generated Yaml file(s): %s, %s", installYamlFilePath, configureYamlFilePath)
	i.deployment = deployment.NewYamlDeployment(cfg.SystemNamespace, installYamlFilePath)

	if err = i.deployment.Deploy(env.Accessor, true, retry.Timeout(cfg.DeployTimeout)); err != nil {
		return nil, err
	}

	if err = env.Accessor.Apply(cfg.SystemNamespace, configureYamlFilePath); err != nil {
		return nil, err
	}

	return i, nil
}

// ID implements resource.Instance
func (i *kubeComponent) ID() resource.ID {
	return i.id
}

func (i *kubeComponent) Settings() Config {
	return i.settings
}

func (i *kubeComponent) Close() (err error) {
	if i.settings.DeployIstio {
		// TODO: There is a problem with  orderly cleanup. Re-enable this once it is fixed. Delete the system namespace
		// instead
		//return i.deployment.Delete(i.environment.Accessor, true, retry.Timeout(s.DeployTimeout))
		err = i.environment.Accessor.DeleteNamespace(i.settings.SystemNamespace)
		if err == nil {
			err = i.environment.Accessor.WaitForNamespaceDeletion(i.settings.SystemNamespace)
		}
	}

	return
}

func (i *kubeComponent) Dump() {
	scopes.CI.Errorf("=== Dumping Istio Deployment State...")

	d, err := i.ctx.CreateTmpDirectory("istio-state")
	if err != nil {
		scopes.CI.Errorf("Unable to create directory for dumping Istio contents: %v", err)
		return
	}

	deployment.DumpPodState(d, i.settings.SystemNamespace, i.environment.Accessor)

	pods, err := i.environment.Accessor.GetPods(i.settings.SystemNamespace)
	if err != nil {
		scopes.CI.Errorf("Unable to get pods from the system namespace: %v", err)
		return
	}

	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			l, err := i.environment.Logs(pod.Namespace, pod.Name, container.Name)
			if err != nil {
				scopes.CI.Errorf("Unable to get logs for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
				continue
			}

			fname := path.Join(d, fmt.Sprintf("%s-%s.log", pod.Name, container.Name))
			if err = ioutil.WriteFile(fname, []byte(l), os.ModePerm); err != nil {
				scopes.CI.Errorf("Unable to write logs for pod/container: %s/%s/%s", pod.Namespace, pod.Name, container.Name)
			}
		}
	}
}
