// Copyright Istio Authors
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

package validation

import (
	gatewayalpha "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayalphavalidation "sigs.k8s.io/gateway-api/apis/v1alpha2/validation"

	"istio.io/istio/pkg/config"
)

// nilSafePtrCast casts t.(*T) safely and returns a T
func nilSafePtrCast[T any](t any) T {
	if t == nil {
		var empty T
		return empty
	}
	return *(t.(*T))
}

// ValidateKubernetesGateway checks config supplied by user
var ValidateKubernetesGateway = registerValidateFunc("ValidateKubernetesGateway",
	func(cfg config.Config) (Warning, error) {
		errs := gatewayalphavalidation.ValidateGateway(&gatewayalpha.Gateway{
			ObjectMeta: cfg.ToObjectMeta(),
			Spec:       nilSafePtrCast[gatewayalpha.GatewaySpec](cfg.Spec),
			Status:     nilSafePtrCast[gatewayalpha.GatewayStatus](cfg.Status),
		})
		return nil, errs.ToAggregate()
	})

// ValidateGatewayClass checks config supplied by user
var ValidateGatewayClass = registerValidateFunc("ValidateGatewayClass",
	func(cfg config.Config) (Warning, error) {
		// TODO: GatewayClass currently only has *update* validation. Istio's webhook server doesn't currently
		// provide old object to the validator, so we can't actually enforce that
		return nil, nil
	})

// ValidateHTTPRoute checks config supplied by user
var ValidateHTTPRoute = registerValidateFunc("ValidateHTTPRoute",
	func(cfg config.Config) (Warning, error) {
		errs := gatewayalphavalidation.ValidateHTTPRoute(&gatewayalpha.HTTPRoute{
			ObjectMeta: cfg.ToObjectMeta(),
			Spec:       nilSafePtrCast[gatewayalpha.HTTPRouteSpec](cfg.Spec),
			Status:     nilSafePtrCast[gatewayalpha.HTTPRouteStatus](cfg.Status),
		})
		return nil, errs.ToAggregate()
	})

// ValidateGRPCRoute checks config supplied by user
var ValidateGRPCRoute = registerValidateFunc("ValidateGRPCRoute",
	func(cfg config.Config) (Warning, error) {
		// Nothing defined by upstream
		return nil, nil
	})

// ValidateTCPRoute checks config supplied by user
var ValidateTCPRoute = registerValidateFunc("ValidateTCPRoute",
	func(cfg config.Config) (Warning, error) {
		errs := gatewayalphavalidation.ValidateTCPRoute(&gatewayalpha.TCPRoute{
			ObjectMeta: cfg.ToObjectMeta(),
			Spec:       nilSafePtrCast[gatewayalpha.TCPRouteSpec](cfg.Spec),
			Status:     nilSafePtrCast[gatewayalpha.TCPRouteStatus](cfg.Status),
		})
		return nil, errs.ToAggregate()
	})

// ValidateTLSRoute checks config supplied by user
var ValidateTLSRoute = registerValidateFunc("ValidateTLSRoute",
	func(cfg config.Config) (Warning, error) {
		errs := gatewayalphavalidation.ValidateTLSRoute(&gatewayalpha.TLSRoute{
			ObjectMeta: cfg.ToObjectMeta(),
			Spec:       nilSafePtrCast[gatewayalpha.TLSRouteSpec](cfg.Spec),
			Status:     nilSafePtrCast[gatewayalpha.TLSRouteStatus](cfg.Status),
		})
		return nil, errs.ToAggregate()
	})

// ValidateUDPRoute checks config supplied by user
var ValidateUDPRoute = registerValidateFunc("ValidateUDPRoute",
	func(cfg config.Config) (Warning, error) {
		errs := gatewayalphavalidation.ValidateUDPRoute(&gatewayalpha.UDPRoute{
			ObjectMeta: cfg.ToObjectMeta(),
			Spec:       nilSafePtrCast[gatewayalpha.UDPRouteSpec](cfg.Spec),
			Status:     nilSafePtrCast[gatewayalpha.UDPRouteStatus](cfg.Status),
		})
		return nil, errs.ToAggregate()
	})
