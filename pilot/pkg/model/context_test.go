// Copyright 2017 Istio Authors
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

package model_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/types"
	"github.com/stretchr/testify/assert"

	"istio.io/istio/pkg/config/labels"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
)

func TestServiceNode(t *testing.T) {
	cases := []struct {
		in  *model.Proxy
		out string
	}{
		{
			in:  &memory.HelloProxyV0,
			out: "sidecar~10.1.1.0~v0.default~default.svc.cluster.local",
		},
		{
			in: &model.Proxy{
				Type:         model.Router,
				ID:           "random",
				IPAddresses:  []string{"10.3.3.3"},
				DNSDomain:    "local",
				IstioVersion: model.MaxIstioVersion,
			},
			out: "router~10.3.3.3~random~local",
		},
		{
			in: &model.Proxy{
				Type:        model.SidecarProxy,
				ID:          "random",
				IPAddresses: []string{"10.3.3.3", "10.4.4.4", "10.5.5.5", "10.6.6.6"},
				DNSDomain:   "local",
				Metadata: map[string]string{
					"INSTANCE_IPS": "10.3.3.3,10.4.4.4,10.5.5.5,10.6.6.6",
				},
				IstioVersion: model.MaxIstioVersion,
			},
			out: "sidecar~10.3.3.3~random~local",
		},
	}

	for _, node := range cases {
		out := node.in.ServiceNode()
		if out != node.out {
			t.Errorf("%#v.ServiceNode() => Got %s, want %s", node.in, out, node.out)
		}
		in, err := model.ParseServiceNodeWithMetadata(node.out, node.in.Metadata)

		if err != nil {
			t.Errorf("ParseServiceNode(%q) => Got error %v", node.out, err)
		}
		if !reflect.DeepEqual(in, node.in) {
			t.Errorf("ParseServiceNode(%q) => Got %#v, want %#v", node.out, in, node.in)
		}
	}
}

func TestParseMetadata(t *testing.T) {
	cases := []struct {
		name     string
		metadata map[string]interface{}
		out      model.Proxy
	}{
		{
			name: "Basic Case",
			out:  model.Proxy{Type: "sidecar", IPAddresses: []string{"1.1.1.1"}, DNSDomain: "domain", ID: "id", IstioVersion: model.MaxIstioVersion},
		},
		{
			name:     "Capture Arbitrary Metadata",
			metadata: map[string]interface{}{"foo": "bar"},
			out: model.Proxy{Type: "sidecar", IPAddresses: []string{"1.1.1.1"}, DNSDomain: "domain", ID: "id", IstioVersion: model.MaxIstioVersion,
				Metadata: map[string]string{
					"foo": "bar",
				}},
		},
		{
			name: "Capture Labels",
			metadata: map[string]interface{}{
				"LABELS": map[string]string{
					"foo": "bar",
				},
			},
			out: model.Proxy{Type: "sidecar", IPAddresses: []string{"1.1.1.1"}, DNSDomain: "domain", ID: "id", IstioVersion: model.MaxIstioVersion,
				Metadata: map[string]string{
					"LABELS": `{"foo":"bar"}`,
				},
				WorkloadLabels: labels.Collection{map[string]string{
					"foo": "bar",
				}}},
		},
		{
			name: "Capture Pod Ports",
			metadata: map[string]interface{}{
				"POD_PORTS": `[{"name":"http","containerPort":8080,"protocol":"TCP"},{"name":"grpc","containerPort":8079,"protocol":"TCP"}]`,
			},
			out: model.Proxy{Type: "sidecar", IPAddresses: []string{"1.1.1.1"}, DNSDomain: "domain", ID: "id", IstioVersion: model.MaxIstioVersion,
				Metadata: map[string]string{
					"POD_PORTS": `[{"name":"http","containerPort":8080,"protocol":"TCP"},{"name":"grpc","containerPort":8079,"protocol":"TCP"}]`,
				}},
		},
	}

	nodeID := "sidecar~1.1.1.1~id~domain"

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := mapToStruct(tt.metadata)
			if err != nil {
				t.Fatalf("failed to setup metadata: %v", err)
			}
			parsed := model.ParseMetadata(meta)
			node, err := model.ParseServiceNodeWithMetadata(nodeID, parsed)
			if err != nil {
				t.Fatalf("failed to parse service node: %v", err)
			}
			if !reflect.DeepEqual(&tt.out, node) {
				t.Errorf("Got %+v, want %+v", node, tt.out)
			}
		})
	}
}

func mapToStruct(msg map[string]interface{}) (*types.Struct, error) {
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}

	pbs := &types.Struct{}
	if err := jsonpb.Unmarshal(bytes.NewBuffer(b), pbs); err != nil {
		return nil, err
	}

	return pbs, nil
}

func TestParsePort(t *testing.T) {
	if port := model.ParsePort("localhost:3000"); port != 3000 {
		t.Errorf("ParsePort(localhost:3000) => Got %d, want 3000", port)
	}
	if port := model.ParsePort("localhost"); port != 0 {
		t.Errorf("ParsePort(localhost) => Got %d, want 0", port)
	}
}

func TestGetOrDefaultFromMap(t *testing.T) {
	meta := map[string]string{"key1": "key1ValueFromMap"}
	assert.Equal(t, "key1ValueFromMap", model.GetOrDefaultFromMap(meta, "key1", "unexpected"))
	assert.Equal(t, "expectedDefaultKey2Value", model.GetOrDefaultFromMap(meta, "key2", "expectedDefaultKey2Value"))
	assert.Equal(t, "expectedDefaultFromNilMap", model.GetOrDefaultFromMap(nil, "key", "expectedDefaultFromNilMap"))
}

func TestProxyVersion_Compare(t *testing.T) {
	type fields struct {
		Major int
		Minor int
		Patch int
	}
	type args struct {
		inv *model.IstioVersion
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   int
	}{
		{
			name:   "greater major",
			fields: fields{Major: 2, Minor: 1, Patch: 1},
			args:   args{&model.IstioVersion{Major: 1, Minor: 2, Patch: 1}},
			want:   1,
		},
		{
			name:   "equal at minor",
			fields: fields{Major: 2, Minor: 1, Patch: 1},
			args:   args{&model.IstioVersion{Major: 2, Minor: 1, Patch: -1}},
			want:   0,
		},
		{
			name:   "less at patch",
			fields: fields{Major: 2, Minor: 1, Patch: 0},
			args:   args{&model.IstioVersion{Major: 2, Minor: 1, Patch: 1}},
			want:   -1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pversion := &model.IstioVersion{
				Major: tt.fields.Major,
				Minor: tt.fields.Minor,
				Patch: tt.fields.Patch,
			}
			if got := pversion.Compare(tt.args.inv); got != tt.want {
				t.Errorf("ProxyVersion.Compare() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseIstioVersion(t *testing.T) {
	type args struct {
		ver string
	}
	tests := []struct {
		name string
		args args
		want *model.IstioVersion
	}{
		{
			name: "major.minor.patch",
			args: args{ver: "1.2.3"},
			want: &model.IstioVersion{Major: 1, Minor: 2, Patch: 3},
		},
		{
			name: "major.minor",
			args: args{ver: "1.2"},
			want: &model.IstioVersion{Major: 1, Minor: 2, Patch: 0},
		},
		{
			name: "release-major.minor-date",
			args: args{ver: "release-1.2-123214234"},
			want: &model.IstioVersion{Major: 1, Minor: 2, Patch: 0},
		},
		{
			name: "master-date",
			args: args{ver: "master-123214234"},
			want: model.MaxIstioVersion,
		},
		{
			name: "junk-major.minor.patch",
			args: args{ver: "junk-1.2.3214234"},
			want: model.MaxIstioVersion,
		},
		{
			name: "junk-garbage",
			args: args{ver: "junk-garbage"},
			want: model.MaxIstioVersion,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := model.ParseIstioVersion(tt.args.ver); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseIstioVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}
