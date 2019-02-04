// Copyright 2018 Istio Authors
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
	"testing"
	"time"

	protoTypes "github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/route/retry"
)

func TestNilRetryShouldReturnDefault(t *testing.T) {
	g := NewGomegaWithT(t)

	// Create a route where no retry policy has been explicitly set.
	route := networking.HTTPRoute{}

	policy := retry.ConvertPolicy(route.Retries)
	g.Expect(policy).To(Not(BeNil()))
	g.Expect(*policy).To(Equal(*retry.DefaultPolicy()))
}

func TestZeroAttemptsShouldReturnNilPolicy(t *testing.T) {
	g := NewGomegaWithT(t)

	// Create a route with a retry policy with zero attempts configured.
	route := networking.HTTPRoute{
		Retries: &networking.HTTPRetry{
			// Explicitly not retrying.
			Attempts: 0,
		},
	}

	policy := retry.ConvertPolicy(route.Retries)
	g.Expect(policy).To(BeNil())
}

func TestRetryWithAllFieldsSet(t *testing.T) {
	g := NewGomegaWithT(t)

	// Create a route with a retry policy with zero attempts configured.
	route := networking.HTTPRoute{
		Retries: &networking.HTTPRetry{
			Attempts: 2,
			RetryOn:  "some,fake,conditions",
			PerTryTimeout: &protoTypes.Duration{
				Seconds: 3,
			},
		},
	}

	policy := retry.ConvertPolicy(route.Retries)
	g.Expect(policy).To(Not(BeNil()))
	g.Expect(policy.RetryOn).To(Equal("some,fake,conditions"))
	g.Expect(*policy.PerTryTimeout).To(Equal(time.Second * 3))
	g.Expect(policy.NumRetries.Value).To(Equal(uint32(2)))
	g.Expect(policy.RetriableStatusCodes).To(Equal(make([]uint32, 0)))
	g.Expect(policy.RetryPriority).To(BeNil())
	g.Expect(policy.HostSelectionRetryMaxAttempts).To(Equal(retry.DefaultPolicy().HostSelectionRetryMaxAttempts))
	g.Expect(policy.RetryHostPredicate).To(Equal(retry.DefaultPolicy().RetryHostPredicate))
}

func TestRetryOnWithEmptyParts(t *testing.T) {
	g := NewGomegaWithT(t)

	// Create a route with a retry policy with zero attempts configured.
	route := networking.HTTPRoute{
		Retries: &networking.HTTPRetry{
			// Explicitly not retrying.
			Attempts: 2,
			RetryOn:  "some,fake,conditions,,,",
		},
	}

	policy := retry.ConvertPolicy(route.Retries)
	g.Expect(policy).To(Not(BeNil()))
	g.Expect(policy.RetryOn).To(Equal("some,fake,conditions"))
	g.Expect(policy.RetriableStatusCodes).To(Equal([]uint32{}))
}

func TestRetryOnWithWhitespace(t *testing.T) {
	g := NewGomegaWithT(t)

	// Create a route with a retry policy with zero attempts configured.
	route := networking.HTTPRoute{
		Retries: &networking.HTTPRetry{
			// Explicitly not retrying.
			Attempts: 2,
			RetryOn: " some,	,fake ,	conditions, ,",
		},
	}

	policy := retry.ConvertPolicy(route.Retries)
	g.Expect(policy).To(Not(BeNil()))
	g.Expect(policy.RetryOn).To(Equal("some,fake,conditions"))
	g.Expect(policy.RetriableStatusCodes).To(Equal([]uint32{}))
}

func TestRetryOnContainingStatusCodes(t *testing.T) {
	g := NewGomegaWithT(t)

	// Create a route with a retry policy with zero attempts configured.
	route := networking.HTTPRoute{
		Retries: &networking.HTTPRetry{
			Attempts: 2,
			RetryOn:  "some,fake,5xx,404,conditions,503",
		},
	}

	policy := retry.ConvertPolicy(route.Retries)
	g.Expect(policy).To(Not(BeNil()))
	g.Expect(policy.RetryOn).To(Equal("some,fake,5xx,conditions"))
	g.Expect(policy.RetriableStatusCodes).To(Equal([]uint32{404, 503}))
}

func TestRetryOnWithInvalidStatusCodesShouldAddToRetryOn(t *testing.T) {
	g := NewGomegaWithT(t)

	// Create a route with a retry policy with zero attempts configured.
	route := networking.HTTPRoute{
		Retries: &networking.HTTPRetry{
			Attempts: 2,
			RetryOn:  "some,fake,conditions,1000",
		},
	}

	policy := retry.ConvertPolicy(route.Retries)
	g.Expect(policy).To(Not(BeNil()))
	g.Expect(policy.RetryOn).To(Equal("some,fake,conditions,1000"))
	g.Expect(policy.RetriableStatusCodes).To(Equal([]uint32{}))
}

func TestMissingRetryOnShouldReturnDefaults(t *testing.T) {
	g := NewGomegaWithT(t)

	// Create a route with a retry policy with zero attempts configured.
	route := networking.HTTPRoute{
		Retries: &networking.HTTPRetry{
			Attempts: 2,
		},
	}

	policy := retry.ConvertPolicy(route.Retries)
	g.Expect(policy).To(Not(BeNil()))
	g.Expect(policy.RetryOn).To(Equal(retry.DefaultPolicy().RetryOn))
	g.Expect(policy.RetriableStatusCodes).To(Equal(retry.DefaultPolicy().RetriableStatusCodes))
}

func TestMissingPerTryTimeoutShouldReturnNil(t *testing.T) {
	g := NewGomegaWithT(t)

	// Create a route with a retry policy with zero attempts configured.
	route := networking.HTTPRoute{
		Retries: &networking.HTTPRetry{
			Attempts: 2,
		},
	}

	policy := retry.ConvertPolicy(route.Retries)
	g.Expect(policy).To(Not(BeNil()))
	g.Expect(policy.PerTryTimeout).To(BeNil())
}
