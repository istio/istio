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
package kubernetesenv_test

import (
	"reflect"
	"testing"

	proto "github.com/gogo/protobuf/proto"

	"istio.io/istio/mixer/adapter/kubernetesenv/config"
	tmpl "istio.io/istio/mixer/adapter/kubernetesenv/template"
)

func Test_StableMarshal(t *testing.T) {

	cases := []struct {
		name       string
		msg1, msg2 proto.Marshaler
	}{
		{
			name: "InstanceParam",
			msg1: &tmpl.InstanceParam{
				SourceUid:      "kubernetes://src.src",
				DestinationUid: "kubernetes://dest.dest",
				AttributeBindings: map[string]string{
					"zulu":     "world",
					"aardvark": "hello",
					"alpha":    "world",
					"Zanzibar": "goodbye cruel",
					"foo":      "...",
					"FOO":      "...",
				},
			},
			msg2: &tmpl.InstanceParam{
				DestinationUid: "kubernetes://dest.dest",
				AttributeBindings: map[string]string{
					"aardvark": "hello",
					"Zanzibar": "goodbye cruel",
					"FOO":      "...",
					"zulu":     "world",
					"alpha":    "world",
					"foo":      "...",
				},
				SourceUid: "kubernetes://src.src",
			},
		},
		{
			name: "ConfigParam",
			msg1: &config.Params{
				KubeconfigPath:             "/some/path",
				ClusterRegistriesNamespace: "istio-system",
			},
			msg2: &config.Params{
				ClusterRegistriesNamespace: "istio-system",
				KubeconfigPath:             "/some/path",
			},
		},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			testMarshalSameOutput(t, v.msg1, v.msg2)
			testSelfMarshal(t, v.msg1)
		})
	}
}

func testMarshalSameOutput(t *testing.T, msg1, msg2 proto.Marshaler) {
	t.Helper()
	msg1Bytes, err := msg1.Marshal()
	if err != nil {
		t.Fatalf("Could not marshal msg1: %v", err)
	}

	msg2Bytes, err := msg2.Marshal()
	if err != nil {
		t.Fatalf("Could not marshal msg2: %v", err)
	}

	if !reflect.DeepEqual(msg1Bytes, msg2Bytes) {
		t.Fatal("Non-stable serialization.")
	}
}

func testSelfMarshal(t *testing.T, msg proto.Marshaler) {
	t.Helper()
	firstOut, err := msg.Marshal()
	if err != nil {
		t.Fatalf("Could not marshal msg: %v", err)
	}
	for i := 0; i < 100; i++ {
		out, err := msg.Marshal()
		if err != nil {
			t.Fatalf("Could not marshal msg: %v", err)
		}
		if !reflect.DeepEqual(firstOut, out) {
			t.Fatal("Non-stable serialization.")
		}
	}
}
