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

package mesh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"istio.io/istio/operator/pkg/manifest"
)

// runManifestGenerateCRDs invokes `istioctl manifest generate-crds` with the
// given input file (may be empty), additional flags, and chart source. The
// chart source is passed via -d so it is not subject to the legacy
// installPackagePath alias used by other helpers.
func runManifestGenerateCRDs(inFile, flags string, chartSource chartSourceType) (string, error) {
	args := "generate-crds -d " + string(chartSource)
	if inFile != "" {
		args += " -f " + inFile
	}
	if flags != "" {
		args += " " + flags
	}
	return runCommand(ManifestCmd, args)
}

// parseCRDManifests parses a multi-document YAML string into Manifests, skipping
// empty documents. It is a thin wrapper around manifest.ParseMultiple that
// fails the test on parse errors.
func parseCRDManifests(t *testing.T, raw string) []manifest.Manifest {
	t.Helper()
	mfs, err := manifest.ParseMultiple(raw)
	if err != nil {
		t.Fatalf("parse manifests: %v", err)
	}
	return mfs
}

// newUnstructured returns an *unstructured.Unstructured populated with the
// given GVK and name. Helper for table-driven unit tests below.
func newUnstructured(apiVersion, kind, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(apiVersion)
	u.SetKind(kind)
	u.SetName(name)
	return u
}

// toManifest wraps newUnstructured output in a manifest.Manifest.
func toManifest(t *testing.T, u *unstructured.Unstructured) manifest.Manifest {
	t.Helper()
	m, err := manifest.FromObject(u)
	if err != nil {
		t.Fatalf("FromObject: %v", err)
	}
	return m
}

func TestManifestGenerateCRDsArgsString(t *testing.T) {
	a := &ManifestGenerateCRDsArgs{
		InFilenames:   []string{"a.yaml", "b.yaml"},
		Set:           []string{"a=b", "c=d"},
		Force:         true,
		ManifestsPath: "/charts",
		Revision:      "canary",
		Output:        "/tmp/out.yaml",
	}
	got := a.String()
	wants := []string{
		"InFilenames:", "[a.yaml b.yaml]",
		"Set:", "[a=b c=d]",
		"Force:", "true",
		"ManifestsPath:", "/charts",
		"Revision:", "canary",
		"Output:", "/tmp/out.yaml",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("String() output missing %q:\n%s", w, got)
		}
	}

	// Zero-value args should still print all field labels without panicking.
	zero := (&ManifestGenerateCRDsArgs{}).String()
	for _, label := range []string{"InFilenames:", "Set:", "Force:", "ManifestsPath:", "Revision:", "Output:"} {
		if !strings.Contains(zero, label) {
			t.Errorf("zero-value String() missing %q:\n%s", label, zero)
		}
	}
}

func TestFilterCRDs(t *testing.T) {
	crd1 := toManifest(t, newUnstructured("apiextensions.k8s.io/v1", "CustomResourceDefinition", "first.example.com"))
	crd2 := toManifest(t, newUnstructured("apiextensions.k8s.io/v1", "CustomResourceDefinition", "second.example.com"))
	cm := toManifest(t, newUnstructured("v1", "ConfigMap", "test-cm"))
	sa := toManifest(t, newUnstructured("v1", "ServiceAccount", "test-sa"))
	// Object that shares the kind name but lives in a different group should
	// not be treated as a CRD.
	notACRD := toManifest(t, newUnstructured("not-apiextensions.example.com/v1", "CustomResourceDefinition", "imposter"))

	tests := []struct {
		name      string
		input     []manifest.ManifestSet
		wantNames []string
	}{
		{
			name: "single set with mixed kinds",
			input: []manifest.ManifestSet{
				{Manifests: []manifest.Manifest{crd1, cm, sa}},
			},
			wantNames: []string{"first.example.com"},
		},
		{
			name: "multiple sets containing multiple CRDs",
			input: []manifest.ManifestSet{
				{Manifests: []manifest.Manifest{crd1, cm}},
				{Manifests: []manifest.Manifest{sa, crd2}},
			},
			wantNames: []string{"first.example.com", "second.example.com"},
		},
		{
			name: "no CRDs",
			input: []manifest.ManifestSet{
				{Manifests: []manifest.Manifest{cm, sa}},
			},
			wantNames: nil,
		},
		{
			name:      "empty input",
			input:     nil,
			wantNames: nil,
		},
		{
			name: "wrong group is excluded",
			input: []manifest.ManifestSet{
				{Manifests: []manifest.Manifest{notACRD, crd1}},
			},
			wantNames: []string{"first.example.com"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filterCRDs(tc.input)
			if len(got) != len(tc.wantNames) {
				t.Fatalf("filterCRDs returned %d items, want %d (%v)", len(got), len(tc.wantNames), tc.wantNames)
			}
			for i, name := range tc.wantNames {
				if got[i].GetName() != name {
					t.Errorf("entry %d: got name %q, want %q", i, got[i].GetName(), name)
				}
				gvk := got[i].GroupVersionKind()
				if gvk.Group != crdGroup || gvk.Kind != crdKind {
					t.Errorf("entry %d: got %s/%s, want %s/%s", i, gvk.Group, gvk.Kind, crdGroup, crdKind)
				}
			}
		})
	}
}

func TestManifestGenerateCRDsCmd_Stdout(t *testing.T) {
	out, err := runManifestGenerateCRDs(inFileAbsolutePath("default"), "", liveCharts)
	if err != nil {
		t.Fatalf("unexpected error: %v: %s", err, out)
	}

	objs := parseCRDManifests(t, out)
	if len(objs) == 0 {
		t.Fatalf("expected at least one CRD in stdout, got 0; out=%q", out)
	}
	for _, o := range objs {
		gvk := o.GroupVersionKind()
		if gvk.Group != crdGroup || gvk.Kind != crdKind {
			t.Errorf("non-CRD object in output: %s/%s %s", gvk.Group, gvk.Kind, o.GetName())
		}
	}
	if !strings.HasSuffix(out, YAMLSeparator) {
		t.Errorf("output should end with YAML separator")
	}
}

func TestManifestGenerateCRDsCmd_OutputFile(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "crds.yaml")
	stdout, err := runManifestGenerateCRDs(inFileAbsolutePath("default"), "-o "+outFile, liveCharts)
	if err != nil {
		t.Fatalf("unexpected error: %v: %s", err, stdout)
	}

	// When -o is specified, the rendered manifests must not be echoed to stdout.
	if strings.Contains(stdout, "kind: CustomResourceDefinition") {
		t.Errorf("expected no CRDs on stdout when -o is set, got:\n%s", stdout)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("output file %q is empty", outFile)
	}

	objs := parseCRDManifests(t, string(data))
	if len(objs) == 0 {
		t.Fatalf("expected at least one CRD in output file, got 0")
	}
	for _, o := range objs {
		gvk := o.GroupVersionKind()
		if gvk.Group != crdGroup || gvk.Kind != crdKind {
			t.Errorf("non-CRD object in output file: %s/%s %s", gvk.Group, gvk.Kind, o.GetName())
		}
	}
	if !strings.HasSuffix(string(data), YAMLSeparator) {
		t.Errorf("output file should end with YAML separator")
	}
}

func TestManifestGenerateCRDsCmd_OutputFileWriteError(t *testing.T) {
	// Point -o at a path whose parent directory does not exist so os.WriteFile
	// fails with a useful error.
	bogus := filepath.Join(t.TempDir(), "nonexistent-subdir", "crds.yaml")
	out, err := runManifestGenerateCRDs(inFileAbsolutePath("default"), "-o "+bogus, liveCharts)
	if err == nil {
		t.Fatalf("expected write error, got nil; stdout=%s", out)
	}
	if !strings.Contains(err.Error(), "write CRDs to") {
		t.Errorf("expected error to mention 'write CRDs to', got %v", err)
	}
}

func TestManifestGenerateCRDsCmd_RejectsPositionalArgs(t *testing.T) {
	out, err := runCommand(ManifestCmd, "generate-crds extra-arg -d "+string(liveCharts))
	if err == nil {
		t.Fatalf("expected error for positional args, got nil; out=%s", out)
	}
	if !strings.Contains(err.Error(), "no positional arguments") {
		t.Errorf("expected positional-arg error, got %v", err)
	}
}

func TestManifestGenerateCRDsCmd_NoCRDsError(t *testing.T) {
	// istio-cni.yaml disables the base component, so no CRDs are rendered and
	// the command must surface a clear error rather than emit an empty document.
	out, err := runManifestGenerateCRDs(inFileAbsolutePath("istio-cni"), "", liveCharts)
	if err == nil {
		t.Fatalf("expected error when no CRDs are rendered; out=%s", out)
	}
	if !strings.Contains(err.Error(), "no CustomResourceDefinitions") {
		t.Errorf("expected 'no CustomResourceDefinitions' error, got %v", err)
	}
}

func TestManifestGenerateCRDsCmd_FlagsRegistered(t *testing.T) {
	// Build the cobra command in isolation so we can introspect the flag set
	// without running it.
	cmd := ManifestCmd(nil)
	gen, _, err := cmd.Find([]string{"generate-crds"})
	if err != nil {
		t.Fatalf("find generate-crds subcommand: %v", err)
	}

	wantFlags := []struct {
		name      string
		shorthand string
	}{
		{name: "filename", shorthand: "f"},
		{name: "set", shorthand: "s"},
		{name: "force", shorthand: ""},
		{name: "manifests", shorthand: "d"},
		{name: "revision", shorthand: "r"},
		{name: "output", shorthand: "o"},
	}
	for _, wf := range wantFlags {
		f := gen.Flag(wf.name)
		if f == nil {
			t.Errorf("flag --%s is not registered", wf.name)
			continue
		}
		if f.Shorthand != wf.shorthand {
			t.Errorf("flag --%s: got shorthand %q, want %q", wf.name, f.Shorthand, wf.shorthand)
		}
	}
}
