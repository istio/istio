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

package roundtrip

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	fuzz "github.com/google/gofuzz"
	flag "github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
)

// This is heavily inspired/copied from https://github.com/kubernetes/apimachinery/blob/master/pkg/api/apitesting/roundtrip/roundtrip.go
// A fork was required to support Istio types. Unlike Kubernetes, which has "normal" go structs, Istio types are protobufs
// Part of this means that each field has a bunch of internal proto stuff, like XXX_sizecache. This does not get roundtripped properly,
// and we do not care that it doesn't. As a result, we switch the comparison to use go-cmp/cmp which can handle this.

type InstallFunc func(scheme *runtime.Scheme)

var FuzzIters = flag.Int("fuzz-iters", 100, "How many fuzzing iterations to do.")

func SpecificKind(t *testing.T, gvk schema.GroupVersionKind, scheme *runtime.Scheme, fuzzer *fuzz.Fuzzer) {
	// Try a few times, since runTest uses random values.
	for i := 0; i < *FuzzIters; i++ {
		roundTripOfExternalType(t, scheme, fuzzer, gvk)
		if t.Failed() {
			break
		}
	}
}

// fuzzInternalObject fuzzes an arbitrary runtime object using the appropriate
// fuzzer registered with the apitesting package.
func fuzzInternalObject(t *testing.T, fuzzer *fuzz.Fuzzer, object runtime.Object) runtime.Object {
	fuzzer.Fuzz(object)

	j, err := apimeta.TypeAccessor(object)
	if err != nil {
		t.Fatalf("Unexpected error %v for %#v", err, object)
	}
	j.SetKind("")
	j.SetAPIVersion("")

	return object
}

func roundTripOfExternalType(t *testing.T, scheme *runtime.Scheme, fuzzer *fuzz.Fuzzer, externalGVK schema.GroupVersionKind) {
	object, err := scheme.New(externalGVK)
	if err != nil {
		t.Fatalf("Couldn't make a %v? %v", externalGVK, err)
	}
	typeAcc, err := apimeta.TypeAccessor(object)
	if err != nil {
		t.Fatalf("%q is not a TypeMeta and cannot be tested - add it to nonRoundTrippableInternalTypes: %v", externalGVK, err)
	}

	fuzzInternalObject(t, fuzzer, object)

	typeAcc.SetKind(externalGVK.Kind)
	typeAcc.SetAPIVersion(externalGVK.GroupVersion().String())

	roundTrip(t, scheme, json.NewSerializer(json.DefaultMetaFactory, scheme, scheme, false), object)
}

var cmpOptions = []cmp.Option{
	// Kubernetes will fuzz this for us
	cmp.Comparer(func(a, b metav1.ObjectMeta) bool {
		return true
	}),
	// Allow comparing protobufs
	cmp.Comparer(gogoproto.Equal),
	// Allow nil == empty list or map
	cmpopts.EquateEmpty(),
}

// roundTrip applies a single round-trip test to the given runtime object
// using the given codec.  The round-trip test ensures that an object can be
// deep-copied, converted, marshaled and back without loss of data.
//
// For internal types this means
//
//   internal -> external -> json/protobuf -> external -> internal.
//
// For external types this means
//
//   external -> json/protobuf -> external.
func roundTrip(t *testing.T, scheme *runtime.Scheme, codec runtime.Codec, object runtime.Object) {
	printer := spew.ConfigState{DisableMethods: true}
	original := object

	// deep copy the original object
	object = object.DeepCopyObject()
	name := reflect.TypeOf(object).Elem().Name()
	if diff := cmp.Diff(original, object, cmpOptions...); diff != "" {
		t.Errorf("%v: DeepCopy altered the object, diff: %v", name, diff)
		t.Errorf("%s", spew.Sdump(original))
		t.Errorf("%s", spew.Sdump(object))
		return
	}

	// encode (serialize) the deep copy using the provided codec
	data, err := runtime.Encode(codec, object)
	if err != nil {
		if runtime.IsNotRegisteredError(err) {
			t.Logf("%v: not registered: %v (%s)", name, err, printer.Sprintf("%#v", object))
		} else {
			t.Errorf("%v: %v (%s)", name, err, printer.Sprintf("%#v", object))
		}
		return
	}

	// ensure that the deep copy is equal to the original; neither the deep
	// copy or conversion should alter the object
	// TODO eliminate this global
	if diff := cmp.Diff(original, object, cmpOptions...); diff != "" {
		t.Errorf("%v: encode altered the object, diff: %v", name, diff)
		return
	}

	// encode (serialize) a second time to verify that it was not varying
	secondData, err := runtime.Encode(codec, object)
	if err != nil {
		if runtime.IsNotRegisteredError(err) {
			t.Logf("%v: not registered: %v (%s)", name, err, printer.Sprintf("%#v", object))
		} else {
			t.Errorf("%v: %v (%s)", name, err, printer.Sprintf("%#v", object))
		}
		return
	}

	// serialization to the wire must be stable to ensure that we don't write twice to the DB
	// when the object hasn't changed.
	if !bytes.Equal(data, secondData) {
		t.Errorf("%v: serialization is not stable: %s", name, printer.Sprintf("%#v", object))
	}

	// decode (deserialize) the encoded data back into an object
	obj2, err := runtime.Decode(codec, data)
	if err != nil {
		t.Errorf("%v: %v\nCodec: %#v\nData: %s\nSource: %#v", name, err, codec, dataAsString(data), printer.Sprintf("%#v", object))
		panic("failed")
	}

	// ensure that the object produced from decoding the encoded data is equal
	// to the original object
	if diff := cmp.Diff(original, obj2, cmpOptions...); diff != "" {
		t.Errorf("%v: diff: %v\nCodec: %#v\nSource:\n\n%#v\n\nEncoded:\n\n%s\n\nFinal:\n\n%#v",
			name, diff, codec, printer.Sprintf("%#v", original), dataAsString(data), printer.Sprintf("%#v", obj2))
		return
	}

	// decode the encoded data into a new object (instead of letting the codec
	// create a new object)
	obj3 := reflect.New(reflect.TypeOf(object).Elem()).Interface().(runtime.Object)
	if err := runtime.DecodeInto(codec, data, obj3); err != nil {
		t.Errorf("%v: %v", name, err)
		return
	}

	// special case for kinds which are internal and external at the same time (many in meta.k8s.io are). For those
	// runtime.DecodeInto above will return the external variant and set the APIVersion and kind, while the input
	// object might be internal. Hence, we clear those values for obj3 for that case to correctly compare.
	intAndExt, err := internalAndExternalKind(scheme, object)
	if err != nil {
		t.Errorf("%v: %v", name, err)
		return
	}
	if intAndExt {
		typeAcc, err := apimeta.TypeAccessor(object)
		if err != nil {
			t.Fatalf("%v: error accessing TypeMeta: %v", name, err)
		}
		if len(typeAcc.GetAPIVersion()) == 0 {
			typeAcc, err := apimeta.TypeAccessor(obj3)
			if err != nil {
				t.Fatalf("%v: error accessing TypeMeta: %v", name, err)
			}
			typeAcc.SetAPIVersion("")
			typeAcc.SetKind("")
		}
	}

	// ensure that the new runtime object is equal to the original after being
	// decoded into
	if diff := cmp.Diff(original, obj3, cmpOptions...); diff != "" {
		t.Errorf("%v: diff: %v\nCodec: %#v", name, diff, codec)
		return
	}

	// do structure-preserving fuzzing of the deep-copied object. If it shares anything with the original,
	// the deep-copy was actually only a shallow copy. Then original and obj3 will be different after fuzzing.
	// NOTE: we use the encoding+decoding here as an alternative, guaranteed deep-copy to compare against.
	fuzzer.ValueFuzz(object)
	if diff := cmp.Diff(original, obj3, cmpOptions...); diff != "" {
		t.Errorf("%v: fuzzing a copy altered the original, diff: %v", name, diff)
		return
	}
}

func internalAndExternalKind(scheme *runtime.Scheme, object runtime.Object) (bool, error) {
	kinds, _, err := scheme.ObjectKinds(object)
	if err != nil {
		return false, err
	}
	internal, external := false, false
	for _, k := range kinds {
		if k.Version == runtime.APIVersionInternal {
			internal = true
		} else {
			external = true
		}
	}
	return internal && external, nil
}

// dataAsString returns the given byte array as a string; handles detecting
// protocol buffers.
func dataAsString(data []byte) string {
	dataString := string(data)
	if !strings.HasPrefix(dataString, "{") {
		dataString = "\n" + hex.Dump(data)
		proto.NewBuffer(make([]byte, 0, 1024)).DebugPrint("decoded object", data)
	}
	return dataString
}
