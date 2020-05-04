package istio

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/environment/native"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"
)

// ClaimSystemNamespace creates, or claims if existing, the namespace for the Istio system components from the environment.
func ClaimSystemNamespace(ctx resource.Context) (namespace.Instance, error) {
	switch ctx.Environment().EnvironmentName() {
	case environment.Kube:
		istioCfg, err := DefaultConfig(ctx)
		if err != nil {
			return nil, err
		}
		return namespace.Claim(ctx, istioCfg.SystemNamespace, false, istioCfg.CustomSidecarInjectorNamespace)
	case environment.Native:
		ns := ctx.Environment().(*native.Environment).SystemNamespace
		return namespace.Claim(ctx, ns, false, "")
	default:
		return nil, resource.UnsupportedEnvironment(ctx.Environment())
	}
}

// ClaimSystemNamespaceOrFail calls ClaimSystemNamespace, failing the test if an error occurs.
func ClaimSystemNamespaceOrFail(t test.Failer, ctx resource.Context) namespace.Instance {
	t.Helper()
	i, err := ClaimSystemNamespace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return i
}
