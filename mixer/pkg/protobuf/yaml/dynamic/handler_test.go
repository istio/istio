// Copyright 2018 Istio Authors.
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

package dynamic

import (
	"testing"
	"istio.io/istio/mixer/pkg/lang/compiled"
	protoyaml "istio.io/istio/mixer/pkg/protobuf/yaml"
	"istio.io/istio/mixer/pkg/adapter"
	"github.com/gogo/protobuf/types"
	"istio.io/istio/mixer/template/metric"
	"istio.io/api/policy/v1beta1"
)

func TestEncodeCheckRequest(t *testing.T) {
	fds, err := protoyaml.GetFileDescSet("../../../../template/metric/template_handler_service.descriptor_set")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	compiler := compiled.NewBuilder(StatdardVocabulary())
	res := protoyaml.NewResolver(fds)

	b := NewEncoderBuilder(res, compiler, true)
	var inst *Svc

	if inst, err = RemoteAdapterSvc("", res); err != nil {
		t.Fatalf("failed to get service:%v", err)
	}

	var re *RequestEncoder
	adapterConfig := &types.Any{
		TypeUrl: "@abc",
		Value:   []byte("abcd"),
	}
	re, err = buildRequestEncoder(b, inst.InputType, &adapter.DynamicHandler{
		Adapter: &adapter.Dynamic{

		},
		AdapterConfig: adapterConfig,
	})

	if err != nil {
		t.Fatalf("unable build request encoder: %v", err)
	}

	dedupString := "dedupString"

	ed0 := &metric.InstanceMsg{
		Name: "inst0",
		Value: &v1beta1.Value{
			Value: &v1beta1.Value_StringValue{
				StringValue: "aaaaaaaaaaaaaaaa",
			},
		},
	}
	ed1 := &metric.InstanceMsg{
		Name: "inst1",
		Value: &v1beta1.Value{
			Value: &v1beta1.Value_DoubleValue{
				DoubleValue: float64(1.1111),
			},
		},
	}

	want := &metric.HandleMetricRequest{
		Instances: []*metric.InstanceMsg{
			ed0,
			ed1,
		},
		AdapterConfig: adapterConfig,
		DedupId: dedupString,
	}

	got := &metric.HandleMetricRequest{}

	eed0, _ := ed0.Marshal()
	eed1, _ := ed1.Marshal()

	br, err1 := re.encodeRequest(nil, dedupString, eed0, eed1)
	if err1 != nil {
		t.Fatalf("unable to encode request: %v", err1)
	}

	if err := got.Unmarshal(br); err != nil {
		wantba, _ := want.Marshal()
		t.Logf("\n got(%d):%v\nwant(%d):%v", len(br), br, len(wantba), wantba)
		t.Logf("\need0:%v\need1:%v", eed0, eed1)
		t.Fatalf("unable to unmarshal: %v", err)
	}

	expectEqual(got, want, t)
}