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

// nolint: golint
package fuzz

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"sync"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
	"github.com/davecgh/go-spew/spew"
	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"

	clientextensions "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	clientnetworkingalpha "istio.io/client-go/pkg/apis/networking/v1alpha3"
	clientnetworkingbeta "istio.io/client-go/pkg/apis/networking/v1beta1"
	clientsecurity "istio.io/client-go/pkg/apis/security/v1beta1"
	clienttelemetry "istio.io/client-go/pkg/apis/telemetry/v1alpha1"
	"istio.io/istio/pkg/config/schema/collections"
)

var (
	scheme  = runtime.NewScheme()
	initter sync.Once
)

func initRoundTrip() {
	clientnetworkingalpha.AddToScheme(scheme)
	clientnetworkingbeta.AddToScheme(scheme)
	clientsecurity.AddToScheme(scheme)
	clientextensions.AddToScheme(scheme)
	clienttelemetry.AddToScheme(scheme)
}

// FuzzRoundtrip tests whether the pilot CRDs
// can be encoded and decoded.
func FuzzCRDRoundtrip(data []byte) int {
	initter.Do(initRoundTrip)
	if len(data) < 100 {
		return 0
	}

	// select a target:
	r := collections.Pilot.All()[int(data[0])%len(collections.Pilot.All())]
	gvk := r.Resource().GroupVersionKind()
	kgvk := schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	}
	object, err := scheme.New(kgvk)
	if err != nil {
		return 0
	}

	typeAcc, err := apimeta.TypeAccessor(object)
	if err != nil {
		panic(fmt.Sprintf("%q is not a TypeMeta and cannot be tested - add it to nonRoundTrippableInternalTypes: %v\n", kgvk, err))
	}
	f := fuzz.NewConsumer(data[1:])
	f.AllowUnexportedFields()
	err = f.GenerateStruct(object)
	if err != nil {
		return 0
	}
	err = checkForNilValues(object)
	if err != nil {
		return 0
	}
	typeAcc.SetKind(kgvk.Kind)
	typeAcc.SetAPIVersion(kgvk.GroupVersion().String())

	roundTrip(json.NewSerializer(json.DefaultMetaFactory, scheme, scheme, false), object)
	return 1
}

// roundTrip performs the roundtrip of the object.
func roundTrip(codec runtime.Codec, object runtime.Object) {
	printer := spew.ConfigState{DisableMethods: true}

	// deep copy the original object
	object = object.DeepCopyObject()
	name := reflect.TypeOf(object).Elem().Name()

	// encode (serialize) the deep copy using the provided codec
	data, err := runtime.Encode(codec, object)
	if err != nil {
		return
	}

	// encode (serialize) a second time to verify that it was not varying
	secondData, err := runtime.Encode(codec, object)
	if err != nil {
		panic("This should not fail since we are encoding for the second time")
	}

	// serialization to the wire must be stable to ensure that we don't write twice to the DB
	// when the object hasn't changed.
	if !bytes.Equal(data, secondData) {
		panic(fmt.Sprintf("%v: serialization is not stable: %s\n", name, printer.Sprintf("%#v", object)))
	}

	// decode (deserialize) the encoded data back into an object
	obj2, err := runtime.Decode(codec, data)
	if err != nil {
		panic(fmt.Sprintf("%v: %v\nCodec: %#v\nData: %s\nSource: %#v\n", name, err, codec, dataAsString(data), printer.Sprintf("%#v", object)))
	}

	// decode the encoded data into a new object (instead of letting the codec
	// create a new object)
	obj3 := reflect.New(reflect.TypeOf(object).Elem()).Interface().(runtime.Object)
	if err := runtime.DecodeInto(codec, data, obj3); err != nil {
		panic(fmt.Sprintf("%v: %v\n", name, err))
	}

	// ensure that the object produced from decoding the encoded data is equal
	// to the original object
	if diff := cmp.Diff(obj2, obj3, protocmp.Transform(), cmpopts.EquateNaNs()); diff != "" {
		panic("These should not be different: " + diff)
	}
}

// dataAsString is a simple helper.
func dataAsString(data []byte) string {
	dataString := string(data)
	if !strings.HasPrefix(dataString, "{") {
		dataString = "\n" + hex.Dump(data)
		proto.NewBuffer(make([]byte, 0, 1024)).DebugPrint("decoded object", data)
	}
	return dataString
}

// checkForNilValues is a helper to check for nil
// values in the runtime objects.
// This part only converts the interface to a reflect.Value.
func checkForNilValues(targetStruct interface{}) error {
	v := reflect.ValueOf(targetStruct)
	e := v.Elem()
	err := checkForNil(e)
	if err != nil {
		return err
	}
	return nil
}

// Checks for nil values in a reflect.Value.
func checkForNil(e reflect.Value) error {
	switch e.Kind() {
	case reflect.Struct:
		for i := 0; i < e.NumField(); i++ {
			err := checkForNil(e.Field(i))
			if err != nil {
				return err
			}
		}
	case reflect.Array, reflect.Slice:
		for i := 0; i < e.Len(); i++ {
			err := checkForNil(e.Index(i))
			if err != nil {
				return err
			}
		}
	case reflect.Map:
		if e.IsNil() {
			return fmt.Errorf("field is nil")
		}
		for _, k := range e.MapKeys() {
			if e.IsNil() {
				return fmt.Errorf("field is nil")
			}
			err := checkForNil(e.MapIndex(k))
			if err != nil {
				return err
			}
		}
	case reflect.Ptr:
		if e.IsNil() {
			return fmt.Errorf("field is nil")
		}

		err := checkForNil(e.Elem())
		if err != nil {
			return err
		}
	default:
		return nil
	}
	return nil
}
