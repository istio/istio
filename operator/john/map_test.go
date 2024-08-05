package john

import (
	"istio.io/istio/pkg/test/util/assert"
	"testing"
)

func TestMakePatch(t *testing.T) {
	data := map[string]string{"hello": "world"}
	cases := []struct {
		name   string
		inPath string
		inData any
		out    string
	}{
		{
			name:   "simple",
			inPath: "spec",
			inData: data,
			out:    "",
		},
		{
			name:   "array",
			inPath: "top.[0]",
			inData: data,
			out:    "",
		},
		{
			name:   "kv",
			inPath: "env.[name:POD_NAME].value",
			inData: data,
			out:    "",
		},
		{
			name:   "escape kv",
			inPath: "env.[name:foo\\.bar].value",
			inData: "hi",
			out:    `{"env":[{"name":"foo\\.bar","value":"hi"}]}`,
		},
		{
			name:   "delete kv last",
			inPath: "env.[name:POD_NAME]",
			inData: nil,
			out:    `{"env":[{"$patch":"delete","name":"POD_NAME"}]}`,
		},
		{
			name:   "set kv primitive",
			inPath: "spec.ports.[name:https-dns].port",
			inData: 11111,
			out:    `{"spec":{"ports":[{"name":"https-dns","port":11111}]}}`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out := MakePatch(tt.inData, tt.inPath)
			assert.Equal(t, tt.out, out)
		})
	}
}
