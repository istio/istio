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

package schema_test

import (
	"math/rand"
	"regexp"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	fuzz "github.com/google/gofuzz"
	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	metafuzzer "k8s.io/apimachinery/pkg/apis/meta/fuzzer"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	authentication "istio.io/api/authentication/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	security "istio.io/api/security/v1beta1"
	clientnetworkingalpha "istio.io/client-go/pkg/apis/networking/v1alpha3"
	clientnetworkingbeta "istio.io/client-go/pkg/apis/networking/v1beta1"
	clientsecurity "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	istiofuzz "istio.io/istio/pkg/config/schema/fuzz"
)

// This test exercises round tripping of marshaling/unmarshaling of all of our CRDs, based on fuzzing
// This approach is heavily adopted from Kubernetes own fuzzing of their resources.
func TestRoundtripFuzzing(t *testing.T) {
	for _, r := range collections.Pilot.All() {
		t.Run(r.VariableName(), func(t *testing.T) {
			fz := createFuzzer()
			t.Parallel()
			gvk := r.Resource().GroupVersionKind()
			kgvk := schema.GroupVersionKind{
				Group:   gvk.Group,
				Version: gvk.Version,
				Kind:    gvk.Kind,
			}
			istiofuzz.RoundTrip(t, kgvk, scheme, fz)
		})
	}
}

// This test exercises validation does not panic based on fuzzed inputs
func TestValidationFuzzing(t *testing.T) {
	fz := createFuzzer()
	for _, r := range collections.Pilot.All() {
		t.Run(r.VariableName(), func(t *testing.T) {
			var iobj *config.Config
			defer func() {
				if err := recover(); err != nil {
					logPanic(t, err)
					t.Fatalf("panic on process %#v", iobj)
				}
			}()
			for i := 0; i < *istiofuzz.FuzzIters; i++ {
				gvk := r.Resource().GroupVersionKind()
				kgvk := schema.GroupVersionKind{
					Group:   gvk.Group,
					Version: gvk.Version,
					Kind:    gvk.Kind,
				}
				obj := istiofuzz.Fuzz(t, kgvk, scheme, fz)
				iobj = crdclient.TranslateObject(obj, gvk, "cluster.local")
				_, _ = r.Resource().ValidateConfig(*iobj)
			}
		})
	}
}

var scheme = runtime.NewScheme()

func init() {
	clientnetworkingalpha.AddToScheme(scheme)
	clientnetworkingbeta.AddToScheme(scheme)
	clientsecurity.AddToScheme(scheme)
}

func createFuzzer() *fuzz.Fuzzer {
	fuzzerFuncs := fuzzer.MergeFuzzerFuncs(metafuzzer.Funcs, fixProtoFuzzer)
	codecs := serializer.NewCodecFactory(scheme)
	seed := time.Now().UTC().UnixNano()
	fz := fuzzer.
		FuzzerFor(fuzzerFuncs, rand.NewSource(seed), codecs).
		SkipFieldsWithPattern(regexp.MustCompile(`^XXX_`))
	return fz
}

// logPanic logs the caller tree when a panic occurs.
func logPanic(t *testing.T, r interface{}) {
	// Same as stdlib http server code. Manually allocate stack trace buffer size
	// to prevent excessively large logs
	const size = 64 << 10
	stacktrace := make([]byte, size)
	stacktrace = stacktrace[:goruntime.Stack(stacktrace, false)]
	t.Errorf("Observed a panic: %#v (%v)\n%s", r, r, stacktrace)
}

// Some proto types cause issues with the fuzzing. These custom fuzzers basically just skip anything with issues
func fixProtoFuzzer(codecs serializer.CodecFactory) []interface{} {
	return []interface{}{
		// This will generate invalid durations - the ranges on the seconds/nanoseconds is bounded
		func(pb *types.Duration, c fuzz.Continue) {
			*pb = types.Duration{}
		},
		// Cannot handle enums, or interfaces in general. See https://github.com/google/gofuzz/issues/27
		// TODO this effectively skips all of these. We should have real fuzzing occur here, just need to add custom logic
		// for the interface types.
		func(x *networking.LoadBalancerSettings, c fuzz.Continue) {
			*x = networking.LoadBalancerSettings{}
		},
		func(t *networking.EnvoyFilter_EnvoyConfigObjectMatch, c fuzz.Continue) {
			*t = networking.EnvoyFilter_EnvoyConfigObjectMatch{}
		},
		func(t *networking.HTTPFaultInjection_Abort, c fuzz.Continue) {
			*t = networking.HTTPFaultInjection_Abort{}
		},
		func(t *networking.HTTPFaultInjection_Delay, c fuzz.Continue) {
			*t = networking.HTTPFaultInjection_Delay{}
		},
		func(t *networking.LoadBalancerSettings, c fuzz.Continue) {
			*t = networking.LoadBalancerSettings{}
		},
		func(t *networking.LoadBalancerSettings_ConsistentHashLB, c fuzz.Continue) {
			*t = networking.LoadBalancerSettings_ConsistentHashLB{}
		},
		func(t *authentication.PeerAuthenticationMethod, c fuzz.Continue) {
			*t = authentication.PeerAuthenticationMethod{}
		},
		func(t *networking.PortSelector, c fuzz.Continue) {
			*t = networking.PortSelector{}
		},
		func(t *networking.StringMatch, c fuzz.Continue) {
			*t = networking.StringMatch{}
		},
		func(t *authentication.StringMatch, c fuzz.Continue) {
			*t = authentication.StringMatch{}
		},
		func(t *types.Timestamp, c fuzz.Continue) {
			*t = types.Timestamp{}
		},
		func(t *types.Value, c fuzz.Continue) {
			*t = types.Value{Kind: &types.Value_StringValue{StringValue: ""}}
		},
		func(t *networking.ReadinessProbe, c fuzz.Continue) {
			t.FailureThreshold = c.Int31()
			t.SuccessThreshold = c.Int31()
			t.InitialDelaySeconds = c.Int31()
			t.PeriodSeconds = c.Int31()
			t.TimeoutSeconds = c.Int31()
			if c.RandBool() {
				hc := &networking.HTTPHealthCheckConfig{}
				c.Fuzz(hc)
				t.HealthCheckMethod = &networking.ReadinessProbe_HttpGet{HttpGet: hc}
			} else {
				if c.RandBool() {
					hc := &networking.TCPHealthCheckConfig{}
					c.Fuzz(hc)
					t.HealthCheckMethod = &networking.ReadinessProbe_TcpSocket{TcpSocket: hc}
				} else {
					hc := &networking.ExecHealthCheckConfig{}
					c.Fuzz(hc)
					t.HealthCheckMethod = &networking.ReadinessProbe_Exec{Exec: hc}
				}
			}
		},
		func(t *security.AuthorizationPolicy, c fuzz.Continue) {
			*t = security.AuthorizationPolicy{}
		},
	}
}
