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

package ambient

import (
	"testing"

	"istio.io/api/security/v1beta1"
	apiv1beta1 "istio.io/api/type/v1beta1"
	securityclient "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/util/assert"
)

const (
	rootns string = "test-system"
)

func TestConvertAuthorizationPolicyStatus(t *testing.T) {
	testCases := []struct {
		name                string
		inputAuthzPol       *securityclient.AuthorizationPolicy
		expectStatusMessage *model.StatusMessage
	}{
		{
			name: "no status - targetRef is not for ztunnel",
			inputAuthzPol: &securityclient.AuthorizationPolicy{
				Spec: v1beta1.AuthorizationPolicy{
					TargetRef: &apiv1beta1.PolicyTargetReference{
						Kind: "Service",
						Name: "test-svc",
					},
					Rules: []*v1beta1.Rule{},
				},
			},
			expectStatusMessage: nil,
		},
		{
			name: "no status - policy is fine",
			inputAuthzPol: &securityclient.AuthorizationPolicy{
				Spec: v1beta1.AuthorizationPolicy{
					Rules: []*v1beta1.Rule{
						{
							From: []*v1beta1.Rule_From{
								{
									Source: &v1beta1.Source{
										Principals: []string{
											"trust/ns/echo-1234/",
										},
									},
								},
							},
							To: []*v1beta1.Rule_To{
								{
									Operation: &v1beta1.Operation{
										Ports: []string{
											"http",
										},
									},
								},
							},
						},
					},
				},
			},
			expectStatusMessage: nil,
		},
		{
			name: "unsupported AUDIT action",
			inputAuthzPol: &securityclient.AuthorizationPolicy{
				Spec: v1beta1.AuthorizationPolicy{
					Action: v1beta1.AuthorizationPolicy_AUDIT,
					Rules: []*v1beta1.Rule{
						{
							From: []*v1beta1.Rule_From{
								{
									Source: &v1beta1.Source{
										Principals: []string{
											"trust/ns/echo-1234/",
										},
									},
								},
							},
							To: []*v1beta1.Rule_To{
								{
									Operation: &v1beta1.Operation{
										Ports: []string{
											"http",
										},
									},
								},
							},
						},
					},
				},
			},
			expectStatusMessage: &model.StatusMessage{
				Reason:  "UnsupportedValue",
				Message: "ztunnel does not support the AUDIT action",
			},
		},
		{
			name: "unsupported CUSTOM action",
			inputAuthzPol: &securityclient.AuthorizationPolicy{
				Spec: v1beta1.AuthorizationPolicy{
					Action: v1beta1.AuthorizationPolicy_CUSTOM,
					Rules: []*v1beta1.Rule{
						{
							From: []*v1beta1.Rule_From{
								{
									Source: &v1beta1.Source{
										Principals: []string{
											"trust/ns/echo-1234/",
										},
									},
								},
							},
							To: []*v1beta1.Rule_To{
								{
									Operation: &v1beta1.Operation{
										Ports: []string{
											"http",
										},
									},
								},
							},
						},
					},
				},
			},
			expectStatusMessage: &model.StatusMessage{
				Reason:  "UnsupportedValue",
				Message: "ztunnel does not support the CUSTOM action",
			},
		},
		{
			name: "unsupported HTTP methods",
			inputAuthzPol: &securityclient.AuthorizationPolicy{
				Spec: v1beta1.AuthorizationPolicy{
					Rules: []*v1beta1.Rule{
						{
							From: []*v1beta1.Rule_From{
								{
									Source: &v1beta1.Source{
										RequestPrincipals: []string{
											"example.com/sub-1",
										},
									},
								},
							},
							To: []*v1beta1.Rule_To{
								{
									Operation: &v1beta1.Operation{
										Methods: []string{
											"GET",
										},
									},
								},
							},
							When: []*v1beta1.Condition{
								{
									Key:    "request.auth.presenter",
									Values: []string{"presenter.example.com"},
								},
							},
						},
					},
				},
			},
			expectStatusMessage: &model.StatusMessage{
				Reason: "UnsupportedValue",
				Message: "ztunnel does not support HTTP rules (methods, request.auth.presenter, requestPrincipals require HTTP parsing), in ambient" +
					" mode you must use waypoint proxy to enforce HTTP rules. Allow rules with HTTP attributes will be empty and never match." +
					" This is more restrictive than requested.",
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			_, outStatusMessage := convertAuthorizationPolicy(rootns, test.inputAuthzPol)
			assert.Equal(t, test.expectStatusMessage, outStatusMessage)
		})
	}
}
