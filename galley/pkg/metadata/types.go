// GENERATED FILE -- DO NOT EDIT
//
//go:generate $GOPATH/src/istio.io/istio/galley/tools/gen-meta/gen-meta.sh runtime pkg/metadata/types.go
//

package metadata

import (
	// Pull in all the known proto types to ensure we get their types registered.
	_ "k8s.io/api/extensions/v1beta1"

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

var (

	// AttributeManifest metadata
	AttributeManifest resource.Info

	// DestinationRule metadata
	DestinationRule resource.Info

	// EnvoyFilter metadata
	EnvoyFilter resource.Info

	// Gateway metadata
	Gateway resource.Info

	// HTTPAPISpec metadata
	HTTPAPISpec resource.Info

	// HTTPAPISpecBinding metadata
	HTTPAPISpecBinding resource.Info

	// Handler metadata
	Handler resource.Info

	// IngressSpec metadata
	IngressSpec resource.Info

	// Instance metadata
	Instance resource.Info

	// LegacyMixerResource metadata
	LegacyMixerResource resource.Info

	// Policy metadata
	Policy resource.Info

	// QuotaSpec metadata
	QuotaSpec resource.Info

	// QuotaSpecBinding metadata
	QuotaSpecBinding resource.Info

	// RbacConfig metadata
	RbacConfig resource.Info

	// Rule metadata
	Rule resource.Info

	// ServiceEntry metadata
	ServiceEntry resource.Info

	// ServiceRole metadata
	ServiceRole resource.Info

	// ServiceRoleBinding metadata
	ServiceRoleBinding resource.Info

	// VirtualService metadata
	VirtualService resource.Info
)

func init() {
	b := resource.NewSchemaBuilder()
	AttributeManifest = b.Register("type.googleapis.com/istio.policy.v1beta1.AttributeManifest")
	DestinationRule = b.Register("type.googleapis.com/istio.networking.v1alpha3.DestinationRule")
	EnvoyFilter = b.Register("type.googleapis.com/istio.networking.v1alpha3.EnvoyFilter")
	Gateway = b.Register("type.googleapis.com/istio.networking.v1alpha3.Gateway")
	HTTPAPISpec = b.Register("type.googleapis.com/istio.mixer.v1.config.client.HTTPAPISpec")
	HTTPAPISpecBinding = b.Register("type.googleapis.com/istio.mixer.v1.config.client.HTTPAPISpecBinding")
	Handler = b.Register("type.googleapis.com/istio.policy.v1beta1.Handler")
	IngressSpec = b.Register("type.googleapis.com/k8s.io.api.extensions.v1beta1.IngressSpec")
	Instance = b.Register("type.googleapis.com/istio.policy.v1beta1.Instance")
	LegacyMixerResource = b.Register("type.googleapis.com/istio.mcp.v1alpha1.extensions.LegacyMixerResource")
	Policy = b.Register("type.googleapis.com/istio.authentication.v1alpha1.Policy")
	QuotaSpec = b.Register("type.googleapis.com/istio.mixer.v1.config.client.QuotaSpec")
	QuotaSpecBinding = b.Register("type.googleapis.com/istio.mixer.v1.config.client.QuotaSpecBinding")
	RbacConfig = b.Register("type.googleapis.com/istio.rbac.v1alpha1.RbacConfig")
	Rule = b.Register("type.googleapis.com/istio.policy.v1beta1.Rule")
	ServiceEntry = b.Register("type.googleapis.com/istio.networking.v1alpha3.ServiceEntry")
	ServiceRole = b.Register("type.googleapis.com/istio.rbac.v1alpha1.ServiceRole")
	ServiceRoleBinding = b.Register("type.googleapis.com/istio.rbac.v1alpha1.ServiceRoleBinding")
	VirtualService = b.Register("type.googleapis.com/istio.networking.v1alpha3.VirtualService")

	Types = b.Build()
}
