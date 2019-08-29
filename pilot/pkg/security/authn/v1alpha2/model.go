package v1alpha2

import (
	envoy_jwt "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/jwt_authn/v2alpha"

	"istio.io/istio/pilot/pkg/model"
)

// V1alpha2Policy: DONOTSUBMIT TODO: add other fields and proper name.
type V1alpha2Policy struct {
	jwtProviders map[string]*envoy_jwt.JwtProvider
}

// GetJwtPolicy returns the JWT requirements for the given workload.
func GetJwtPolicy(store model.IstioConfigStore, serviceInstance *model.ServiceInstance) *V1alpha2Policy {
	return nil
}
