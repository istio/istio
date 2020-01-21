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

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/util/retry"

	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/errors"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

type kubeComponent struct {
	id          resource.ID
	settings    Config
	ctx         resource.Context
	environment *kube.Environment
	deployment  *deployment.Instance
}

func DefaultValidatingWebhookConfigurationName(config Config) string {
	if config.IsIstiodEnabled() {
		return fmt.Sprintf("istiod-%v", config.SystemNamespace)
	}
	return "istio-galley"
}

const (
	DefaultMutatingWebhookConfigurationName = "istio-sidecar-injector"
)

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

	// Top-level work dir for Istio deployment.
	workDir, err := ctx.CreateTmpDirectory("istio-deployment")
	if err != nil {
		return nil, err
	}

	// Create helm working dir
	helmWorkDir := path.Join(workDir, "helm")
	if err := os.MkdirAll(helmWorkDir, os.ModePerm); err != nil {
		return nil, err
	}

	// First, generate CRDs.
	crdYaml, err := generateCRDYaml(cfg.CrdsFilesDir)
	if err != nil {
		return nil, err
	}

	// Generate rendered yaml file for Istio, including namespace.
	istioYaml, err := generateIstioYaml(helmWorkDir, cfg)
	if err != nil {
		return nil, err
	}

	// split installation & configuration into two distinct steps, so that we can submit configuration before waiting
	// for Galley to come online.
	installYaml, configureYaml := splitIstioYaml(istioYaml)

	// Write out as files for deployment and debugging purposes.
	crdFile := path.Join(workDir, "crd.yaml")
	if err = ioutil.WriteFile(crdFile, []byte(crdYaml), os.ModePerm); err != nil {
		return nil, fmt.Errorf("unable to write %q: %v", crdFile, err)
	}
	istioFile := path.Join(workDir, "istio.yaml")
	if err = ioutil.WriteFile(istioFile, []byte(istioYaml), os.ModePerm); err != nil {
		return nil, fmt.Errorf("unable to write %q: %v", istioFile, err)
	}
	istioInstallFile := path.Join(workDir, "istio-install-only.yaml")
	if err = ioutil.WriteFile(istioInstallFile, []byte(installYaml), os.ModePerm); err != nil {
		return nil, fmt.Errorf("unable to write %q: %v", istioInstallFile, err)
	}
	istioConfigFile := path.Join(workDir, "istio-config-only.yaml")
	if err = ioutil.WriteFile(istioConfigFile, []byte(configureYaml), os.ModePerm); err != nil {
		return nil, fmt.Errorf("unable to write %q: %v", istioConfigFile, err)
	}
	scopes.CI.Infof("Wrote out istio deployment files at: %s", workDir)

	// Apply CRDs first.
	if err = env.Accessor.Apply("", crdFile); err != nil {
		return nil, err
	}

	// Deploy Istio.
	i.deployment = deployment.NewYamlDeployment(cfg.SystemNamespace, istioInstallFile)
	if err = i.deployment.Deploy(env.Accessor, true, retry.Timeout(cfg.DeployTimeout)); err != nil {
		i.Dump()
		return nil, err
	}

	if !cfg.SkipWaitForValidationWebhook {
		webhookService := "istio-galley"
		if cfg.IsIstiodEnabled() {
			webhookService = "istio-pilot"
		}

		// Wait for Galley & the validation webhook to come online before applying Istio configurations.
		if _, _, err = env.WaitUntilServiceEndpointsAreReady(cfg.SystemNamespace, webhookService); err != nil {
			err = fmt.Errorf("error waiting %s/%v service endpoints: %v", cfg.SystemNamespace, webhookService, err)
			scopes.CI.Info(err.Error())
			i.Dump()
			return nil, err
		}

		// Wait for webhook to come online. The only reliable way to do that is to see if we can submit invalid config.
		err = waitForValidationWebhook(env.Accessor, cfg)
		if err != nil {
			i.Dump()
			return nil, err
		}
	}

	// Then, apply Istio configuration.
	if err = env.Accessor.Apply("", istioConfigFile); err != nil {
		i.Dump()
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

func (i *kubeComponent) Close() error {
	var err *multierror.Error

	if i.settings.DeployIstio {
		// TODO: There is a problem with  orderly cleanup. Re-enable this once it is fixed. Delete the system namespace
		// instead
		// return i.deployment.Delete(i.environment.Accessor, true, retry.Timeout(s.DeployTimeout))
		if i.ctx.Settings().FailOnDeprecation {
			err = multierror.Append(err, i.checkDeprecation())
		}
		err2 := i.environment.Accessor.DeleteNamespace(i.settings.SystemNamespace)
		err = multierror.Append(err, err2)
		if err2 == nil {
			err = multierror.Append(err, i.environment.Accessor.WaitForNamespaceDeletion(i.settings.SystemNamespace))
		}
		// Note: when cleaning up an Istio deployment, ValidatingWebhookConfiguration
		// and MutatingWebhookConfiguration must be cleaned up. Otherwise, next
		// Istio deployment in the cluster will be impacted, causing flaky test results.
		// Clean up ValidatingWebhookConfiguration and MuttatingWebhookConfiguration if they exist
		_ = i.environment.DeleteValidatingWebhook(DefaultValidatingWebhookConfigurationName(i.settings))
		_ = i.environment.DeleteMutatingWebhook(DefaultMutatingWebhookConfigurationName)
	}

	return err.ErrorOrNil()
}

func (i *kubeComponent) checkDeprecation() error {
	pods, err := i.environment.GetPods(i.settings.SystemNamespace)
	if err != nil {
		return fmt.Errorf("could not get pods to inspect for deprecation messages: %v", err)
	}

	var result error
	for _, pod := range pods {
		for _, c := range pod.Spec.Containers {
			if c.Name == "istio-proxy" {
				info := fmt.Sprintf("pod: %s/%s", i.settings.SystemNamespace, pod.Name)
				logs, err := i.environment.Logs(i.settings.SystemNamespace, pod.Name, "istio-proxy", false)
				if err != nil {
					result = multierror.Append(result, fmt.Errorf("could not get proxy logs (%s) to inspect for deprecation messages: %v", info, err))
				} else {
					result = multierror.Append(result, errors.FindDeprecatedMessagesInEnvoyLog(logs, info))
				}
			}
		}
	}

	return result
}

func (i *kubeComponent) Dump() {
	scopes.CI.Errorf("=== Dumping Istio Deployment State...")

	d, err := i.ctx.CreateTmpDirectory("istio-state")
	if err != nil {
		scopes.CI.Errorf("Unable to create directory for dumping Istio contents: %v", err)
		return
	}

	deployment.DumpPodState(d, i.settings.SystemNamespace, i.environment.Accessor)
	deployment.DumpPodEvents(d, i.settings.SystemNamespace, i.environment.Accessor)

	pods, err := i.environment.Accessor.GetPods(i.settings.SystemNamespace)
	if err != nil {
		scopes.CI.Errorf("Unable to get pods from the system namespace: %v", err)
		return
	}

	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			l, err := i.environment.Logs(pod.Namespace, pod.Name, container.Name, false /* previousLog */)
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
