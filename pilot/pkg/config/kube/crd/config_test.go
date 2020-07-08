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

package crd_test

import (
	"math/rand"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	fuzz "github.com/google/gofuzz"
	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"

	authentication "istio.io/api/authentication/v1alpha1"
	mixer "istio.io/api/mixer/v1"
	mixerclient "istio.io/api/mixer/v1/config/client"
	networking "istio.io/api/networking/v1alpha3"
	policy "istio.io/api/policy/v1beta1"
	clientconfig "istio.io/client-go/pkg/apis/config/v1alpha2"
	clientnetworkingalpha "istio.io/client-go/pkg/apis/networking/v1alpha3"
	clientnetworkingbeta "istio.io/client-go/pkg/apis/networking/v1beta1"
	clientsecurity "istio.io/client-go/pkg/apis/security/v1beta1"

	metafuzzer "k8s.io/apimachinery/pkg/apis/meta/fuzzer"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"istio.io/istio/pilot/pkg/config/kube/crd/roundtrip"
	"istio.io/istio/pkg/config/schema/collections"

	"istio.io/istio/pilot/pkg/config/kube/crd"
)

// This test exercises round tripping of marshaling/unmarshaling of all of our CRDs, based on fuzzing
// This approach is heavily adopted from Kubernetes own fuzzing of their resources.
func TestRoundtripFuzzing(t *testing.T) {
	scheme := runtime.NewScheme()
	clientnetworkingalpha.AddToScheme(scheme)
	clientnetworkingbeta.AddToScheme(scheme)
	clientconfig.AddToScheme(scheme)
	clientsecurity.AddToScheme(scheme)

	fuzzerFuncs := fuzzer.MergeFuzzerFuncs(metafuzzer.Funcs, fixProtoFuzzer)
	codecs := serializer.NewCodecFactory(scheme)
	seed := time.Now().UTC().UnixNano()
	fz := fuzzer.
		FuzzerFor(fuzzerFuncs, rand.NewSource(seed), codecs).
		SkipFieldsWithPattern(regexp.MustCompile(`^XXX_`))

	for _, r := range collections.Pilot.All() {
		t.Run(r.VariableName(), func(t *testing.T) {
			gvk := r.Resource().GroupVersionKind()
			kgvk := schema.GroupVersionKind{
				Group:   gvk.Group,
				Version: gvk.Version,
				Kind:    gvk.Kind,
			}
			roundtrip.SpecificKind(t, kgvk, scheme, fz)
		})
	}
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
		func(t *mixerclient.APIKey, c fuzz.Continue) {
			*t = mixerclient.APIKey{}
		},
		func(t *mixer.Attributes_AttributeValue, c fuzz.Continue) {
			*t = mixer.Attributes_AttributeValue{}
		},
		func(t *policy.Authentication, c fuzz.Continue) {
			*t = policy.Authentication{}
		},
		func(t *networking.EnvoyFilter_EnvoyConfigObjectMatch, c fuzz.Continue) {
			*t = networking.EnvoyFilter_EnvoyConfigObjectMatch{}
		},
		func(t *mixerclient.HTTPAPISpecPattern, c fuzz.Continue) {
			*t = mixerclient.HTTPAPISpecPattern{}
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
		func(t *mixerclient.StringMatch, c fuzz.Continue) {
			*t = mixerclient.StringMatch{}
		},
		func(t *authentication.StringMatch, c fuzz.Continue) {
			*t = authentication.StringMatch{}
		},
		func(t *policy.Tls, c fuzz.Continue) {
			*t = policy.Tls{}
		},
		func(t *policy.Value, c fuzz.Continue) {
			*t = policy.Value{}
		},
		func(t *types.Value, c fuzz.Continue) {
			*t = types.Value{Kind: &types.Value_StringValue{StringValue: ""}}
		},
	}
}

func TestKind(t *testing.T) {
	obj := crd.IstioKind{}

	spec := map[string]interface{}{"a": "b"}
	obj.Spec = spec
	if got := obj.GetSpec(); !reflect.DeepEqual(spec, got) {
		t.Errorf("GetSpec() => got %v, want %v", got, spec)
	}

	meta := meta_v1.ObjectMeta{Name: "test"}
	obj.ObjectMeta = meta
	if got := obj.GetObjectMeta(); !reflect.DeepEqual(meta, got) {
		t.Errorf("GetObjectMeta() => got %v, want %v", got, meta)
	}

	if got := obj.DeepCopy(); !reflect.DeepEqual(*got, obj) {
		t.Errorf("DeepCopy() => got %v, want %v", got, obj)
	}

	if got := obj.DeepCopyObject(); !reflect.DeepEqual(got, &obj) {
		t.Errorf("DeepCopyObject() => got %v, want %v", got, obj)
	}

	var empty *crd.IstioKind
	if empty.DeepCopy() != nil {
		t.Error("DeepCopy of nil should return nil")
	}

	if empty.DeepCopyObject() != nil {
		t.Error("DeepCopyObject of nil should return nil")
	}
}
