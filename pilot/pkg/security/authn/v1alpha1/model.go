package v1alpha1

import (
	authn "istio.io/api/authentication/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	authn_model "istio.io/istio/pilot/pkg/security/model"
)

// GetConsolidateAuthenticationPolicy returns the v1alpha1 authentication policy for workload specified by
// hostname (or label selector if specified) and port, if defined.
// It also tries to resolve JWKS URI if necessary.
func GetConsolidateAuthenticationPolicy(store model.IstioConfigStore, serviceInstance *model.ServiceInstance) *authn.Policy {
	service := serviceInstance.Service
	port := serviceInstance.Endpoint.ServicePort
	labels := serviceInstance.Labels

	config := store.AuthenticationPolicyForWorkload(service, labels, port)
	if config != nil {
		policy := config.Spec.(*authn.Policy)
		if err := authn_model.JwtKeyResolver.SetAuthenticationPolicyJwksURIs(policy); err == nil {
			return policy
		}
	}

	return nil
}
