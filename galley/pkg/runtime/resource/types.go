// GENERATED FILE -- DO NOT EDIT
//
//go:generate $GOPATH/src/istio.io/istio/galley/tools/gen-meta/gen-meta.sh runtime pkg/runtime/resource/types.go
//

package resource

import (
	// Pull in all the known proto types to ensure we get their types registered.
	_ "istio.io/api/authentication/v1alpha1"
	_ "istio.io/api/mixer/adapter/model/v1beta1"
	_ "istio.io/api/mixer/v1/config/client"
	_ "istio.io/api/networking/v1alpha3"
	_ "istio.io/api/policy/v1beta1"
	_ "istio.io/api/rbac/v1alpha1"
	_ "istio.io/api/routing/v1alpha1"
)

// Types of known resources.
var Types = NewSchema()

func init() {
	Types.Register("type.googleapis.com/istio.authentication.v1alpha1.Policy", true)
	Types.Register("type.googleapis.com/istio.mixer.adapter.model.v1beta1.Info", true)
	Types.Register("type.googleapis.com/istio.mixer.adapter.model.v1beta1.Template", true)
	Types.Register("type.googleapis.com/istio.mixer.v1.config.client.HTTPAPISpec", true)
	Types.Register("type.googleapis.com/istio.mixer.v1.config.client.HTTPAPISpecBinding", true)
	Types.Register("type.googleapis.com/istio.mixer.v1.config.client.QuotaSpec", true)
	Types.Register("type.googleapis.com/istio.mixer.v1.config.client.QuotaSpecBinding", true)
	Types.Register("type.googleapis.com/istio.networking.v1alpha3.DestinationRule", true)
	Types.Register("type.googleapis.com/istio.networking.v1alpha3.EnvoyFilter", true)
	Types.Register("type.googleapis.com/istio.networking.v1alpha3.Gateway", true)
	Types.Register("type.googleapis.com/istio.networking.v1alpha3.ServiceEntry", true)
	Types.Register("type.googleapis.com/istio.networking.v1alpha3.VirtualService", true)
	Types.Register("type.googleapis.com/istio.policy.v1beta1.AttributeManifest", true)
	Types.Register("type.googleapis.com/istio.policy.v1beta1.Handler", true)
	Types.Register("type.googleapis.com/istio.policy.v1beta1.Instance", true)
	Types.Register("type.googleapis.com/istio.policy.v1beta1.Rule", true)
	Types.Register("type.googleapis.com/istio.rbac.v1alpha1.RbacConfig", false)
	Types.Register("type.googleapis.com/istio.rbac.v1alpha1.ServiceRole", false)
	Types.Register("type.googleapis.com/istio.rbac.v1alpha1.ServiceRoleBinding", false)
}
