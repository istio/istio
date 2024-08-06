package values

import (
	"testing"

	"istio.io/istio/pkg/test/util/assert"
)

func TestSetPath(t *testing.T) {
	fromJson := func(s string) Map {
		m, err := fromJson[Map]([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	cases := []struct {
		name   string
		base   Map
		inPath string
		inData any
		out    string
	}{
		{
			name:   "trivial",
			inPath: "spec",
			inData: 1,
			out:    `{"spec":1}`,
		},
		{
			name:   "simple create",
			inPath: "spec.bar",
			inData: 1,
			out:    `{"spec":{"bar":1}}`,
		},
		{
			name:   "simple merge",
			inPath: "spec.bar",
			base:   Map{"spec": Map{"foo": "baz"}},
			inData: 1,
			out:    `{"spec":{"bar":1,"foo":"baz"}}`,
		},
		{
			name:   "array",
			inPath: "top.[0]",
			inData: 1,
			out:    `{"top":[1]}`,
		},
		{
			name:   "array flattened",
			inPath: "top[0]",
			inData: 1,
			out:    `{"top":[1]}`,
		},
		{
			name:   "array and values",
			inPath: "top.[0].bar",
			inData: 1,
			out:    `{"top":[{"bar":1}]}`,
		},
		{
			name:   "array and values merge",
			inPath: "top.[0].bar",
			base:   fromJson(`{"top":[{"baz":2}]}`),
			inData: 1,
			out:    `{"top":[{"bar":1,"baz":2}]}`,
		},
		{
			name:   "kv set",
			inPath: "env.[name:POD_NAME].value",
			base:   fromJson(`{"env":[{"name":"POD_NAME"}]}`),
			inData: 1,
			out:    `{"env":[{"name":"POD_NAME","value":1}]}`,
		},
		{
			name:   "escape kv",
			inPath: "env.[name:foo\\.bar].value",
			base:   fromJson(`{"env":[{"name":"foo.bar"}]}`),
			inData: "hi",
			out:    `{"env":[{"name":"foo.bar","value":"hi"}]}`,
		},
		{
			name:   "set kv",
			inPath: "spec.ports.[name:https-dns].port",
			base:   fromJson(`{"spec":{"ports":[{"name":"https-dns"}]}}`),
			inData: 11111,
			out:    `{"spec":{"ports":[{"name":"https-dns","port":11111}]}}`,
		},
		{
			name:   "set unmatched kv",
			inPath: "spec.ports.[name:https-dns].port",
			inData: 11111,
			out:    ``,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			m := Map{}
			if tt.base != nil {
				m = tt.base
			}
			err := m.SetPath(tt.inPath, tt.inData)
			if tt.out != "" {
				assert.NoError(t, err)
				assert.Equal(t, tt.out, m.JSON())
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestGetPath(t *testing.T) {
	fromJson := func(s string) Map {
		m, err := fromJson[Map]([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	cases := []struct {
		name string
		base Map
		path string
		out  any
	}{
		{
			name: "trivial",
			base: fromJson(`{"spec":1}`),
			path: "spec",
			out:  float64(1),
		},
		{
			name: "nested",
			path: "spec.bar",
			base: fromJson(`{"spec":{"bar":1}}`),
			out:  float64(1),
		},
		{
			name: "map",
			path: "spec",
			base: fromJson(`{"spec":{"bar":1}}`),
			out:  map[string]any{"bar": float64(1)},
		},
		{
			name: "array",
			path: "top.[0]",
			base: fromJson(`{"top":[1]}`),
			out:  float64(1),
		},
		{
			name: "array out of bounds",
			path: "top.[9]",
			base: fromJson(`{"top":[1]}`),
			out:  nil,
		},
		{
			name: "array and values",
			path: "top.[0].bar",
			base: fromJson(`{"top":[{"bar":1}]}`),
			out:  float64(1),
		},
		{
			name: "kv",
			path: "env.[name:POD_NAME].value",
			base: fromJson(`{"env":[{"name":"POD_NAME","value":1}]}`),
			out:  float64(1),
		},
		{
			name: "escape kv",
			path: "env.[name:foo\\.bar].value",
			base: fromJson(`{"env":[{"name":"foo.bar","value":"hi"}]}`),
			out:  "hi",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			v, _ := tt.base.GetPath(tt.path)
			assert.Equal(t, tt.out, v)
		})
	}
}

func TestParseValue(t *testing.T) {
	tests := []struct {
		desc string
		in   string
		want any
	}{
		{
			desc: "empty",
			in:   "",
			want: "",
		},
		{
			desc: "true",
			in:   "true",
			want: true,
		},
		{
			desc: "false",
			in:   "false",
			want: false,
		},
		{
			desc: "numeric-one",
			in:   "1",
			want: 1,
		},
		{
			desc: "numeric-zero",
			in:   "0",
			want: 0,
		},
		{
			desc: "numeric-large",
			in:   "12345678",
			want: 12345678,
		},
		{
			desc: "numeric-negative",
			in:   "-12345678",
			want: -12345678,
		},
		{
			desc: "float",
			in:   "1.23456",
			want: 1.23456,
		},
		{
			desc: "float-zero",
			in:   "0.00",
			want: 0.00,
		},
		{
			desc: "float-negative",
			in:   "-6.54321",
			want: -6.54321,
		},
		{
			desc: "string",
			in:   "foobar",
			want: "foobar",
		},
		{
			desc: "string-number-prefix",
			in:   "123foobar",
			want: "123foobar",
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := parseValue(tt.in), tt.want; !(got == want) {
				t.Errorf("%s: got:%v, want:%v", tt.desc, got, want)
			}
		})
	}
}
