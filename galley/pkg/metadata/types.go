// GENERATED FILE -- DO NOT EDIT
//
//go:generate $GOPATH/src/istio.io/istio/galley/tools/gen-meta/gen-meta.sh runtime pkg/metadata/types.go
//

package metadata

import (
	// Pull in all the known proto types to ensure we get their types registered.
	_ "istio.io/api/authentication/v1alpha1"
	_ "istio.io/api/mixer/v1/config/client"
	_ "istio.io/api/networking/v1alpha3"
	_ "istio.io/api/policy/v1beta1"
	_ "istio.io/api/rbac/v1alpha1"
	_ "istio.io/istio/galley/pkg/kube/converter/legacy"
	"istio.io/istio/galley/pkg/runtime/resource"
)

// Types of known resources.
var Types *resource.Schema

func init() {
	b := resource.NewSchemaBuilder()
	b.Register("type.googleapis.com/istio.authentication.v1alpha1.Policy", true)
	b.Register("type.googleapis.com/istio.mcp.v1alpha1.extensions.LegacyMixerResource", true)
	b.Register("type.googleapis.com/istio.mixer.v1.config.client.HTTPAPISpec", true)
	b.Register("type.googleapis.com/istio.mixer.v1.config.client.HTTPAPISpecBinding", true)
	b.Register("type.googleapis.com/istio.mixer.v1.config.client.QuotaSpec", true)
	b.Register("type.googleapis.com/istio.mixer.v1.config.client.QuotaSpecBinding", true)
	b.Register("type.googleapis.com/istio.networking.v1alpha3.DestinationRule", true)
	b.Register("type.googleapis.com/istio.networking.v1alpha3.EnvoyFilter", true)
	b.Register("type.googleapis.com/istio.networking.v1alpha3.Gateway", true)
	b.Register("type.googleapis.com/istio.networking.v1alpha3.ServiceEntry", true)
	b.Register("type.googleapis.com/istio.networking.v1alpha3.VirtualService", true)
	b.Register("type.googleapis.com/istio.policy.v1beta1.AttributeManifest", true)
	b.Register("type.googleapis.com/istio.policy.v1beta1.Handler", true)
	b.Register("type.googleapis.com/istio.policy.v1beta1.Instance", true)
	b.Register("type.googleapis.com/istio.policy.v1beta1.Rule", true)
	b.Register("type.googleapis.com/istio.rbac.v1alpha1.RbacConfig", false)
	b.Register("type.googleapis.com/istio.rbac.v1alpha1.ServiceRole", false)
	b.Register("type.googleapis.com/istio.rbac.v1alpha1.ServiceRoleBinding", false)

	Types = b.Build()
}
