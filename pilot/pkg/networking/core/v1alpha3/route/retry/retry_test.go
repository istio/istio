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

package retry_test

import (
	"reflect"
	"testing"
	"time"

	envoyroute "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	previouspriorities "github.com/envoyproxy/go-control-plane/envoy/extensions/retry/priority/previous_priorities/v3"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/route/retry"
	"istio.io/istio/pilot/pkg/networking/util"
)

func TestRetry(t *testing.T) {
	testCases := []struct {
		name       string
		route      *networking.HTTPRoute
		assertFunc func(g *WithT, policy *envoyroute.RetryPolicy)
	}{
		{
			name: "TestNilRetryShouldReturnDefault",
			// Create a route where no retry policy has been explicitly set.
			route: &networking.HTTPRoute{},
			assertFunc: func(g *WithT, policy *envoyroute.RetryPolicy) {
				g.Expect(policy).To(Not(BeNil()))
				g.Expect(policy).To(Equal(retry.DefaultPolicy()))
			},
		},
		{
			name: "TestZeroAttemptsShouldReturnNilPolicy",
			// Create a route with a retry policy with zero attempts configured.
			route: &networking.HTTPRoute{
				Retries: &networking.HTTPRetry{
					// Explicitly not retrying.
					Attempts: 0,
				},
			},
			assertFunc: func(g *WithT, policy *envoyroute.RetryPolicy) {
				g.Expect(policy).To(BeNil())
			},
		},
		{
			name: "TestRetryWithAllFieldsSet",
			// Create a route with a retry policy with all fields configured.
			route: &networking.HTTPRoute{
				Retries: &networking.HTTPRetry{
					Attempts:      2,
					RetryOn:       "some,fake,conditions",
					PerTryTimeout: durationpb.New(time.Second * 3),
				},
			},
			assertFunc: func(g *WithT, policy *envoyroute.RetryPolicy) {
				g.Expect(policy).To(Not(BeNil()))
				g.Expect(policy.RetryOn).To(Equal("some,fake,conditions"))
				g.Expect(policy.PerTryTimeout).To(Equal(durationpb.New(time.Second * 3)))
				g.Expect(policy.NumRetries.Value).To(Equal(uint32(2)))
				g.Expect(policy.RetriableStatusCodes).To(Equal(make([]uint32, 0)))
				g.Expect(policy.RetryPriority).To(BeNil())
				g.Expect(policy.HostSelectionRetryMaxAttempts).To(Equal(retry.DefaultPolicy().HostSelectionRetryMaxAttempts))
				g.Expect(policy.RetryHostPredicate).To(Equal(retry.DefaultPolicy().RetryHostPredicate))
			},
		},
		{
			name: "TestRetryOnWithEmptyParts",
			// Create a route with a retry policy with empty retry conditions configured.
			route: &networking.HTTPRoute{
				Retries: &networking.HTTPRetry{
					// Explicitly not retrying.
					Attempts: 2,
					RetryOn:  "some,fake,conditions,,,",
				},
			},
			assertFunc: func(g *WithT, policy *envoyroute.RetryPolicy) {
				g.Expect(policy).To(Not(BeNil()))
				g.Expect(policy.RetryOn).To(Equal("some,fake,conditions"))
				g.Expect(policy.RetriableStatusCodes).To(Equal([]uint32{}))
			},
		},
		{
			name: "TestRetryOnWithRetriableStatusCodes",
			// Create a route with a retry policy with retriable status code.
			route: &networking.HTTPRoute{
				Retries: &networking.HTTPRetry{
					// Explicitly not retrying.
					Attempts: 2,
					RetryOn:  "gateway-error,retriable-status-codes,503",
				},
			},
			assertFunc: func(g *WithT, policy *envoyroute.RetryPolicy) {
				g.Expect(policy).To(Not(BeNil()))
				g.Expect(policy.RetryOn).To(Equal("gateway-error,retriable-status-codes"))
				g.Expect(policy.RetriableStatusCodes).To(Equal([]uint32{503}))
			},
		},
		{
			name: "TestRetryOnWithWhitespace",
			// Create a route with a retry policy with retryOn having white spaces.
			route: &networking.HTTPRoute{
				Retries: &networking.HTTPRetry{
					// Explicitly not retrying.
					Attempts: 2,
					RetryOn: " some,	,fake ,	conditions, ,",
				},
			},
			assertFunc: func(g *WithT, policy *envoyroute.RetryPolicy) {
				g.Expect(policy).To(Not(BeNil()))
				g.Expect(policy.RetryOn).To(Equal("some,fake,conditions"))
				g.Expect(policy.RetriableStatusCodes).To(Equal([]uint32{}))
			},
		},
		{
			name: "TestRetryOnContainingStatusCodes",
			// Create a route with a retry policy with status codes.
			route: &networking.HTTPRoute{
				Retries: &networking.HTTPRetry{
					Attempts: 2,
					RetryOn:  "some,fake,5xx,404,conditions,503",
				},
			},
			assertFunc: func(g *WithT, policy *envoyroute.RetryPolicy) {
				g.Expect(policy).To(Not(BeNil()))
				g.Expect(policy.RetryOn).To(Equal("some,fake,5xx,conditions,retriable-status-codes"))
				g.Expect(policy.RetriableStatusCodes).To(Equal([]uint32{404, 503}))
			},
		},
		{
			name: "TestRetryOnWithInvalidStatusCodesShouldAddToRetryOn",
			// Create a route with a retry policy with invalid status codes.
			route: &networking.HTTPRoute{
				Retries: &networking.HTTPRetry{
					Attempts: 2,
					RetryOn:  "some,fake,conditions,1000",
				},
			},
			assertFunc: func(g *WithT, policy *envoyroute.RetryPolicy) {
				g.Expect(policy).To(Not(BeNil()))
				g.Expect(policy.RetryOn).To(Equal("some,fake,conditions,1000"))
				g.Expect(policy.RetriableStatusCodes).To(Equal([]uint32{}))
			},
		},
		{
			name: "TestMissingRetryOnShouldReturnDefaults",
			// Create a route with a retry policy with two attempts configured.
			route: &networking.HTTPRoute{
				Retries: &networking.HTTPRetry{
					Attempts: 2,
				},
			},
			assertFunc: func(g *WithT, policy *envoyroute.RetryPolicy) {
				g.Expect(policy).To(Not(BeNil()))
				g.Expect(policy.RetryOn).To(Equal(retry.DefaultPolicy().RetryOn))
				g.Expect(policy.RetriableStatusCodes).To(Equal(retry.DefaultPolicy().RetriableStatusCodes))
			},
		},
		{
			name: "TestMissingPerTryTimeoutShouldReturnNil",
			// Create a route with a retry policy without per try timeout.
			route: &networking.HTTPRoute{
				Retries: &networking.HTTPRetry{
					Attempts: 2,
				},
			},
			assertFunc: func(g *WithT, policy *envoyroute.RetryPolicy) {
				g.Expect(policy).To(Not(BeNil()))
				g.Expect(policy.PerTryTimeout).To(BeNil())
			},
		},
		{
			name: "TestRetryRemoteLocalities",
			// Create a route with a retry policy with RetryRemoteLocalities enabled.
			route: &networking.HTTPRoute{
				Retries: &networking.HTTPRetry{
					Attempts: 2,
					RetryRemoteLocalities: &wrappers.BoolValue{
						Value: true,
					},
				},
			},
			assertFunc: func(g *WithT, policy *envoyroute.RetryPolicy) {
				g.Expect(policy).To(Not(BeNil()))
				g.Expect(policy.RetryOn).To(Equal(retry.DefaultPolicy().RetryOn))
				g.Expect(policy.RetriableStatusCodes).To(Equal(retry.DefaultPolicy().RetriableStatusCodes))

				previousPrioritiesConfig := &previouspriorities.PreviousPrioritiesConfig{
					UpdateFrequency: int32(2),
				}
				expected := &envoyroute.RetryPolicy_RetryPriority{
					Name: "envoy.retry_priorities.previous_priorities",
					ConfigType: &envoyroute.RetryPolicy_RetryPriority_TypedConfig{
						TypedConfig: util.MessageToAny(previousPrioritiesConfig),
					},
				}
				if !reflect.DeepEqual(policy.RetryPriority, expected) {
					t.Fatalf("Expected %v, actual %v", expected, policy.RetryPriority)
				}
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			policy := retry.ConvertPolicy(tc.route.Retries)
			if tc.assertFunc != nil {
				tc.assertFunc(g, policy)
			}
		})
	}
}
