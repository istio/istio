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
	b.Register("type.googleapis.com/istio.authentication.v1alpha1.Policy")
	b.Register("type.googleapis.com/istio.mcp.v1alpha1.extensions.LegacyMixerResource")
	b.Register("type.googleapis.com/istio.mixer.v1.config.client.HTTPAPISpec")
	b.Register("type.googleapis.com/istio.mixer.v1.config.client.HTTPAPISpecBinding")
	b.Register("type.googleapis.com/istio.mixer.v1.config.client.QuotaSpec")
	b.Register("type.googleapis.com/istio.mixer.v1.config.client.QuotaSpecBinding")
	b.Register("type.googleapis.com/istio.networking.v1alpha3.DestinationRule")
	b.Register("type.googleapis.com/istio.networking.v1alpha3.EnvoyFilter")
	b.Register("type.googleapis.com/istio.networking.v1alpha3.Gateway")
	b.Register("type.googleapis.com/istio.networking.v1alpha3.ServiceEntry")
	b.Register("type.googleapis.com/istio.networking.v1alpha3.VirtualService")
	b.Register("type.googleapis.com/istio.policy.v1beta1.AttributeManifest")
	// b.Register("type.googleapis.com/istio.policy.v1beta1.Handler")
	b.Register("type.googleapis.com/istio.policy.v1beta1.Instance")
	b.Register("type.googleapis.com/istio.policy.v1beta1.Rule")
	b.Register("type.googleapis.com/istio.rbac.v1alpha1.RbacConfig")
	b.Register("type.googleapis.com/istio.rbac.v1alpha1.ServiceRole")
	b.Register("type.googleapis.com/istio.rbac.v1alpha1.ServiceRoleBinding")

	Types = b.Build()
}
