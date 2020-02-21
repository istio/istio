package util

import (
	"testing"

	"github.com/kylelemons/godebug/diff"
)

func TestProcessYAMLText(t *testing.T) {
	testRules := []*Rule{
		{
			// Add "labels" key under "metadata" if it doesn't exist.
			Path: PathFromString("metadata"),
			Key:  "labels",
		},
		{
			// Add "add-key: add-value" under "metadata.labels" if it doesn't exist.
			Path: PathFromString("metadata.labels"),
			Key:  "add-key",
			Val:  "add-value",
		},
		{
			// Replace value at "metadata.labels.replace" with "new-value".
			Path:     PathFromString("metadata.labels.replace"),
			Val:      "new-value",
			Op:       REPLACE,
			Position: AT,
		},
	}
	tests := []struct {
		desc  string
		in    string
		rules []*Rule
		want  string
	}{
		{
			desc:  "empty",
			in:    "",
			rules: testRules,
			want:  "",
		},
		{
			desc: "no labels",
			in: `
kind: ClusterRole
metadata:
  name: cr1
rules: # comment
- apiGroups:
  - networking.istio.io
  resources:
  - '*'
`,
			rules: testRules,
			want: `
kind: ClusterRole
metadata:
  labels:
    add-key: add-value
  name: cr1
rules: # comment
- apiGroups:
  - networking.istio.io
  resources:
  - '*'
`,
		},
		{
			desc: "has labels",
			in: `
kind: ClusterRole
metadata:
  labels:
    aaa: bbb
  name: cr1
rules: # comment
- apiGroups:
  - networking.istio.io
  resources:
  - '*'
`,
			rules: testRules,
			want: `
kind: ClusterRole
metadata:
  labels:
    add-key: add-value
    aaa: bbb
  name: cr1
rules: # comment
- apiGroups:
  - networking.istio.io
  resources:
  - '*'
`,
		},
		{
			desc: "replace",
			in: `
kind: ClusterRole
metadata:
  labels:
    replace: old-value
  name: cr1
`,
			rules: testRules,
			want: `
kind: ClusterRole
metadata:
  labels:
    add-key: add-value
    replace: new-value
  name: cr1
`,
		},
		{
			desc: "mixed manifest",
			in: `
kind: ClusterRole
metadata:
  name: cr1
  labels:
    abc: xyz
rules: # comment
- apiGroups:
  - networking.istio.io
  resources:
  - '*'
---
# comment
---
kind: ClusterRole
metadata:
  name: cr2
rules:
- apiGroups:  # comment
  - networking.istio.io
  resources:
  - '*'
`,
			rules: testRules,
			want: `
kind: ClusterRole
metadata:
  name: cr1
  labels:
    add-key: add-value
    abc: xyz
rules: # comment
- apiGroups:
  - networking.istio.io
  resources:
  - '*'
---
# comment
---
kind: ClusterRole
metadata:
  labels:
    add-key: add-value
  name: cr2
rules:
- apiGroups:  # comment
  - networking.istio.io
  resources:
  - '*'
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := ProcessYAMLText(tt.in, tt.rules)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("%s: got: \n%s\n\ndiff (-got, +want):\n%s", tt.desc, got, diff.Diff(got, tt.want))
			}
		})
	}
}
