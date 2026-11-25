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

package main

import (
	"strings"
	"testing"

	"istio.io/istio/operator/pkg/apis" // nolint: depguard
	"istio.io/istio/pkg/test/util/assert"
)

func TestRefsInLine(t *testing.T) {
	cases := []struct {
		name string
		line string
		want []string
	}{
		{
			name: "plain",
			line: `image: "{{ .Values.global.hub }}/{{ .Values.image }}"`,
			want: []string{"global.hub", "image"},
		},
		{
			name: "root scope",
			line: `{{- if $.Values.pilot.env }}`,
			want: []string{"pilot.env"},
		},
		{
			name: "nil safe",
			line: `{{- with (coalesce ($.Values).platform ($.Values.global).platform) }}`,
			want: []string{"global", "platform", "global.platform"},
		},
		{
			name: "dynamic lookups are not checked",
			line: `{{- $gateway := index .Values "gateways" "istio-ingressgateway" }}`,
			want: nil,
		},
		{
			name: "values alone",
			line: `{{ toYaml .Values | indent 4 }}`,
			want: nil,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, refsInLine(tt.line), tt.want)
		})
	}
}

func TestBlankComments(t *testing.T) {
	in := `a: {{ .Values.real }}
{{- /* a comment about .Values.fake
spanning lines */}}
b: {{ .Values.alsoReal }}`
	got := blankComments(in)
	assert.Equal(t, strings.Count(got, "\n"), strings.Count(in, "\n"), "line count is preserved")

	var refs []string
	for _, line := range strings.Split(got, "\n") {
		refs = append(refs, refsInLine(line)...)
	}
	assert.Equal(t, refs, []string{"real", "alsoReal"})
}

func TestResolveRef(t *testing.T) {
	values := (&apis.Values{}).ProtoReflect().Descriptor()
	cases := []struct {
		name      string
		fieldPath string
		path      string
		wantErr   bool
	}{
		{name: "chart value", fieldPath: "pilot", path: "hub"},
		{name: "shared value", fieldPath: "pilot", path: "global.hub"},
		{name: "nested chart value", fieldPath: "pilot", path: "pilot.env"},
		{name: "top level value", fieldPath: "", path: "profile"},
		{name: "into a struct", fieldPath: "pilot", path: "env.SOME_VARIABLE"},
		{name: "into a free form value", fieldPath: "ztunnel", path: "anything.at.all"},
		{name: "unknown chart value", fieldPath: "pilot", path: "notAField", wantErr: true},
		{name: "unknown shared value", fieldPath: "pilot", path: "global.notAField", wantErr: true},
		{name: "scalar is not a message", fieldPath: "pilot", path: "global.hub.nested", wantErr: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := resolveRef(values, tt.fieldPath, tt.path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
