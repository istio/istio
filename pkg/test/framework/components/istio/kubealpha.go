package istio

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"sync"

	"istio.io/istio/pkg/test/deployment"
	ienv "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

func getNamespaces(cfg Config) []string {
	allNamespace := []string{}
	for _, c := range cfg.InstallComponents {
		switch c {
		case Base:
			allNamespace = append(allNamespace, cfg.IstioNamespace, cfg.ConfigNamespace)
		case Ingress:
			allNamespace = append(allNamespace, cfg.IngressNamespace)
		case Egress:
			allNamespace = append(allNamespace, cfg.EgressNamespace)
		case Telemetry:
			allNamespace = append(allNamespace, cfg.TelemetryNamespace)
		case Policy:
			allNamespace = append(allNamespace, cfg.PolicyNamespace)
		}
	}
	return allNamespace
}

type installerComponent struct {
	id          resource.ID
	settings    Config
	ctx         resource.Context
	environment *kube.Environment
	deployment  *deployment.Instance
}

var _ io.Closer = &installerComponent{}
var _ Instance = &installerComponent{}
var _ resource.Dumper = &installerComponent{}

// ID implements resource.Instance
func (i *installerComponent) ID() resource.ID {
	return i.id
}

func (i *installerComponent) Settings() Config {
	return i.settings
}

func (i *installerComponent) Close() error {

	if !i.settings.DeployIstio {
		return nil
	}
	wg := &sync.WaitGroup{}
	for _, ns := range getNamespaces(i.settings) {
		ns := ns
		wg.Add(1)
		go func() {
			err := i.environment.Accessor.DeleteNamespace(ns)
			if err == nil {
				err = i.environment.Accessor.WaitForNamespaceDeletion(ns)
			}
			wg.Done()
		}()
	}
	wg.Wait()
	return nil
}

func (i *installerComponent) Dump() {
	scopes.CI.Errorf("=== Dumping Istio Deployment State...")

	d, err := i.ctx.CreateTmpDirectory("istio-state")
	if err != nil {
		scopes.CI.Errorf("Unable to create directory for dumping Istio contents: %v", err)
		return
	}
	for _, ns := range getNamespaces(i.settings) {
		deployment.DumpPodState(d, ns, i.environment.Accessor)
		deployment.DumpPodEvents(d, ns, i.environment.Accessor)

		pods, err := i.environment.Accessor.GetPods(ns)
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
}

func deployAlphaInstall(ctx resource.Context, env *kube.Environment, cfg Config) (Instance, error) {
	scopes.CI.Infof("=== Istio Component Config ===")
	scopes.CI.Infof("\n%s", cfg.String())
	scopes.CI.Infof("================================")

	i := &installerComponent{
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
	crdYaml, err := generateCRDYaml(path.Join(ienv.IstioAlphaInstallDir, "crds/files"))
	if err != nil {
		return nil, err
	}

	scopes.CI.Infof("Applying CRDs: %v", workDir)

	// Apply CRDs first.
	if _, err = env.Accessor.ApplyContents("", crdYaml); err != nil {
		return nil, err
	}

	// Restructure components for easier lookup
	components := map[InstallComponent]struct{}{}
	for _, c := range cfg.InstallComponents {
		components[c] = struct{}{}
	}

	if _, f := components[Base]; f {
		// Generate rendered yaml file for Istio, including namespace.
		if err := applyAlphaInstall(env.Accessor, helmWorkDir, cfg.IstioNamespace, "security/citadel", cfg); err != nil {
			return nil, err
		}

		if err := applyAlphaInstall(env.Accessor, helmWorkDir, cfg.ConfigNamespace, "istio-control/istio-config", cfg); err != nil {
			return nil, err
		}

		// Wait for Galley & the validation webhook to come online before applying Istio configurations.
		if _, _, err = env.WaitUntilServiceEndpointsAreReady(cfg.ConfigNamespace, "istio-galley"); err != nil {
			err = fmt.Errorf("error waiting %s/istio-galley service endpoints: %v", cfg.SystemNamespace, err)
			scopes.CI.Info(err.Error())
			return nil, err
		}

		// Wait for webhook to come online. The only reliable way to do that is to see if we can submit invalid config.
		err = waitForValidationWebhook(env.Accessor)
		if err != nil {
			return nil, err
		}

		if err := applyAlphaInstall(env.Accessor, helmWorkDir, cfg.ConfigNamespace, "istio-control/istio-discovery", cfg); err != nil {
			return nil, err
		}
		if err := applyAlphaInstall(env.Accessor, helmWorkDir, cfg.ConfigNamespace, "istio-control/istio-autoinject", cfg); err != nil {
			return nil, err
		}
	}

	if _, f := components[Ingress]; f {
		if err := applyAlphaInstall(env.Accessor, helmWorkDir, cfg.IngressNamespace, "gateways/istio-ingress", cfg); err != nil {
			return nil, err
		}
	}
	if _, f := components[Egress]; f {
		if err := applyAlphaInstall(env.Accessor, helmWorkDir, cfg.IngressNamespace, "gateways/istio-egress", cfg); err != nil {
			return nil, err
		}
	}

	if _, f := components[Telemetry]; f {
		if err := applyAlphaInstall(env.Accessor, helmWorkDir, cfg.TelemetryNamespace, "istio-telemetry/prometheus", cfg); err != nil {
			return nil, err
		}
		if err := applyAlphaInstall(env.Accessor, helmWorkDir, cfg.TelemetryNamespace, "istio-telemetry/mixer-telemetry", cfg); err != nil {
			return nil, err
		}
	}

	if _, f := components[Policy]; f {
		if err := applyAlphaInstall(env.Accessor, helmWorkDir, cfg.PolicyNamespace, "istio-policy", cfg); err != nil {
			return nil, err
		}
	}

	return i, nil
}
