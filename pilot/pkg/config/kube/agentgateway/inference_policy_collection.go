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

package agentgateway

import (
	"fmt"

	"github.com/agentgateway/agentgateway/go/api"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"

	"istio.io/istio/pkg/kube/krt"
)

// InferencePolicyCollection watches InferencePools and generates per-pool inference routing
// and backend TLS policies as AgwResources to be pushed via XDS.
//
// Each InferencePool produces two policies:
//   - An inference routing policy targeting the pool's virtual service hostname,
//     configuring the EPP (Endpoint Picker Protocol) service and failure mode.
//   - A backend TLS policy targeting the EPP service, setting INSECURE_ALL
//     verification since the EPP uses gRPC without certificate verification disabled.
func InferencePolicyCollection(
	pools krt.Collection[*inferencev1.InferencePool],
	domainSuffix string,
	opts krt.OptionsBuilder,
) krt.Collection[AgwResource] {
	return krt.NewManyCollection(pools, func(ctx krt.HandlerContext, pool *inferencev1.InferencePool) []AgwResource {
		return translatePoliciesForInferencePool(pool, domainSuffix)
	}, opts.WithName("InferencePolicies")...)
}

func translatePoliciesForInferencePool(pool *inferencev1.InferencePool, domainSuffix string) []AgwResource {
	epr := pool.Spec.EndpointPickerRef
	if epr.Port == nil {
		logger.Warnf("InferencePool %s/%s has no endpointPickerRef port, skipping policy generation", pool.Namespace, pool.Name)
		return nil
	}

	eppName := string(epr.Name)
	eppPort := uint32(epr.Port.Number)
	poolHostname := fmt.Sprintf("%s.%s.inference.%s", pool.Name, pool.Namespace, domainSuffix)
	eppHostname := fmt.Sprintf("%s.%s.svc.%s", eppName, pool.Namespace, domainSuffix)

	// Map failure mode
	failureMode := api.BackendPolicySpec_InferenceRouting_FAIL_CLOSED
	if epr.FailureMode == inferencev1.EndpointPickerFailOpen {
		failureMode = api.BackendPolicySpec_InferenceRouting_FAIL_OPEN
	}

	// Policy 1: Inference routing — tells the proxy to use the EPP for endpoint selection
	inferencePolicy := &api.Policy{
		Key: fmt.Sprintf("%s/%s:inference", pool.Namespace, pool.Name),
		Target: &api.PolicyTarget{
			Kind: &api.PolicyTarget_Service{
				Service: &api.PolicyTarget_ServiceTarget{
					Namespace: pool.Namespace,
					Hostname:  poolHostname,
				},
			},
		},
		Kind: &api.Policy_Backend{
			Backend: &api.BackendPolicySpec{
				Kind: &api.BackendPolicySpec_InferenceRouting_{
					InferenceRouting: &api.BackendPolicySpec_InferenceRouting{
						EndpointPicker: &api.BackendReference{
							Kind: &api.BackendReference_Service_{
								Service: &api.BackendReference_Service{
									Hostname:  eppHostname,
									Namespace: pool.Namespace,
								},
							},
							Port: eppPort,
						},
						FailureMode: failureMode,
					},
				},
			},
		},
	}

	// Policy 2: Backend TLS for EPP — the EPP gRPC service uses plaintext
	tlsPolicy := &api.Policy{
		Key: fmt.Sprintf("%s/%s:inferencetls", pool.Namespace, pool.Name),
		Target: &api.PolicyTarget{
			Kind: &api.PolicyTarget_Service{
				Service: &api.PolicyTarget_ServiceTarget{
					Namespace: pool.Namespace,
					Hostname:  eppHostname,
					Port:      &eppPort,
				},
			},
		},
		Kind: &api.Policy_Backend{
			Backend: &api.BackendPolicySpec{
				Kind: &api.BackendPolicySpec_BackendTls{
					BackendTls: &api.BackendPolicySpec_BackendTLS{
						Verification: api.BackendPolicySpec_BackendTLS_INSECURE_ALL,
					},
				},
			},
		},
	}

	return []AgwResource{
		{Resource: ToAgwResource(inferencePolicy)},
		{Resource: ToAgwResource(tlsPolicy)},
	}
}
