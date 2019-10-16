package k8s

import (
	"github.com/hashicorp/go-multierror"
	"istio.io/istio/pkg/kube/inject"
)

// Injector implements the sidecar injection - specific to K8S.
// Technically it doesn't require K8S - but it's not used outside.

// StartInjector will register the injector handle. No webhook patching or reconcile.
// For now use a different port.
// TLS will be handled by Envoy
func StartInjector(stop chan struct{}) error {
	// TODO: modify code to allow startup without TLS ( let envoy handle it)
	// TODO: switch readiness to common http based.

	parameters := inject.WebhookParameters{
		ConfigFile:          "./var/lib/istio/inject/injection-template.yaml",
		ValuesFile:          "./var/lib/istio/install/values.yaml",
		MeshFile:            "./var/lib/istio/config/mesh",
		CertFile:            "./var/run/secrets/istio-dns/cert-chain.pem",
		KeyFile:             "./var/run/secrets/istio-dns/key.pem",
		Port:                15017,
		HealthCheckInterval: 0,
		HealthCheckFile:     "",
		MonitoringPort:      0,
	}
	wh, err := inject.NewWebhook(parameters)
	if err != nil {
		return multierror.Prefix(err, "failed to create injection webhook")
	}

	go wh.Run(stop)
	return nil
}
