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

package bootstrap

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/golang/protobuf/jsonpb"
	. "github.com/onsi/gomega"
	"k8s.io/kubectl/pkg/util/fieldpath"

	"istio.io/istio/pilot/pkg/model"
)

func TestParseDownwardApi(t *testing.T) {
	cases := []struct {
		name string
		m    map[string]string
	}{
		{
			"empty",
			map[string]string{},
		},
		{
			"single",
			map[string]string{"foo": "bar"},
		},
		{
			"multi",
			map[string]string{
				"app":               "istio-ingressgateway",
				"chart":             "gateways",
				"heritage":          "Tiller",
				"istio":             "ingressgateway",
				"pod-template-hash": "54756dbcf9",
			},
		},
		{
			"multi line",
			map[string]string{
				"config": `foo: bar
other: setting`,
				"istio": "ingressgateway",
			},
		},
		{
			"weird values",
			map[string]string{
				"foo": `a1_-.as1`,
				"bar": `a=b`,
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Using the function kubernetes actually uses to write this, we do a round trip of
			// map -> file -> map and ensure the input and output are the same
			got, err := ParseDownwardAPI(fieldpath.FormatMap(tt.m))
			if !reflect.DeepEqual(got, tt.m) {
				t.Fatalf("expected %v, got %v with err: %v", tt.m, got, err)
			}
		})
	}
}

func TestGetNodeMetaData(t *testing.T) {
	inputOwner := "test"
	inputWorkloadName := "workload"

	expectOwner := "test"
	expectWorkloadName := "workload"

	os.Setenv(IstioMetaPrefix+"OWNER", inputOwner)
	os.Setenv(IstioMetaPrefix+"WORKLOAD_NAME", inputWorkloadName)

	node, err := GetNodeMetaData(MetadataOptions{
		ID:   "test",
		Envs: os.Environ(),
	})

	g := NewWithT(t)
	g.Expect(err).Should(BeNil())
	g.Expect(node.Metadata.Owner).To(Equal(expectOwner))
	g.Expect(node.Metadata.WorkloadName).To(Equal(expectWorkloadName))
	g.Expect(node.RawMetadata["OWNER"]).To(Equal(expectOwner))
	g.Expect(node.RawMetadata["WORKLOAD_NAME"]).To(Equal(expectWorkloadName))
}

func TestConvertNodeMetadata(t *testing.T) {
	node := &model.Node{
		ID: "test",
		Metadata: &model.BootstrapNodeMetadata{
			NodeMetadata: model.NodeMetadata{
				ProxyConfig: &model.NodeMetaProxyConfig{
					ServiceCluster: "cluster",
				},
			},
			Owner: "real-owner",
		},
		RawMetadata: map[string]interface{}{},
	}
	node.Metadata.Owner = "real-owner"
	node.RawMetadata["OWNER"] = "fake-owner"
	node.RawMetadata["UNKNOWN"] = "new-field"
	node.RawMetadata["A"] = 1
	node.RawMetadata["B"] = map[string]interface{}{"b": 1}

	out := ConvertNodeToXDSNode(node)
	{
		buf := &bytes.Buffer{}
		if err := (&jsonpb.Marshaler{OrigName: true}).Marshal(buf, out); err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}
		// nolint: lll
		want := "{\"id\":\"test\",\"cluster\":\"cluster\",\"metadata\":{\"A\":1,\"B\":{\"b\":1},\"OWNER\":\"real-owner\",\"PROXY_CONFIG\":{\"serviceCluster\":\"cluster\"},\"UNKNOWN\":\"new-field\"}}"
		if want != buf.String() {
			t.Fatalf("ConvertNodeToXDSNode: got %q, want %q", buf.String(), want)
		}
	}

	node2 := ConvertXDSNodeToNode(out)
	{
		got, err := json.Marshal(node2)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}
		// nolint: lll
		want := "{\"ID\":\"test\",\"Metadata\":{\"PROXY_CONFIG\":{\"serviceCluster\":\"cluster\"},\"OWNER\":\"real-owner\"},\"RawMetadata\":null,\"Locality\":null}"
		if want != string(got) {
			t.Fatalf("ConvertXDSNodeToNode: got %q, want %q", string(got), want)
		}
	}
}
