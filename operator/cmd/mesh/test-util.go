package mesh

import (
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"github.com/onsi/gomega/types"
	labels2 "k8s.io/apimachinery/pkg/labels"

	"istio.io/istio/operator/pkg/compare"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pkg/test"
)

// PathValue is a path/value type.
type PathValue struct {
	path  string
	value interface{}
}

// String implements the Stringer interface.
func (pv *PathValue) String() string {
	return fmt.Sprintf("%s:%v", pv.path, pv.value)
}

// HavePathValueEqual matches object.K8sObjects against a PathValue. All objects must match the PathValue.
func HavePathValueEqual(expected interface{}) types.GomegaMatcher {
	return &HavePathValueEqualMatcher{
		expected: expected,
	}
}

// HavePathValueEqualMatcher is a matcher type for HavePathValueEqual.
type HavePathValueEqualMatcher struct {
	expected interface{}
}

// Match implements the Matcher interface.
func (m *HavePathValueEqualMatcher) Match(actual interface{}) (bool, error) {
	objs := actual.(object.K8sObjects)
	pv := m.expected.(PathValue)
	for _, o := range objs {
		got, f, err := tpath.GetPathContext(o.UnstructuredObject().UnstructuredContent(), util.PathFromString(pv.path))
		if err != nil || !f {
			return false, err
		}
		if !reflect.DeepEqual(got.Node, pv.value) {
			return false, nil
		}
	}
	return true, nil
}

// FailureMessage implements the Matcher interface.
func (m *HavePathValueEqualMatcher) FailureMessage(actual interface{}) string {
	pv := m.expected.(PathValue)
	objs := actual.(object.K8sObjects)
	return fmt.Sprintf("Expected the following objects to have path=value %s=%v\n\n%v", pv.path, pv.value, objs)
}

// NegatedFailureMessage implements the Matcher interface.
func (m *HavePathValueEqualMatcher) NegatedFailureMessage(actual interface{}) string {
	pv := m.expected.(PathValue)
	objs := actual.(object.K8sObjects)
	return fmt.Sprintf("Expected the following objects not to have path=value %s=%v\n\n%v", pv.path, pv.value, objs)
}

// deployments returns all the Deployment objects in the manifest.
func deployments(t test.Failer, manifest string) object.K8sObjects {
	objs, err := object.ParseK8sObjectsFromYAMLManifest(filteredManifest(t, manifest, "Deployment:*:*", ""))
	if err != nil {
		t.Fatal(err)
	}
	return objs
}

// filteredManifest returns a manifest that has been filtered with selected and ignored resource expressions.
func filteredManifest(t test.Failer, manifest, selectResources, ignoreResources string) string {
	ret, err := compare.FilterManifest(manifest, selectResources, ignoreResources)
	if err != nil {
		t.Fatal(err)
	}
	return ret
}

// filteredManifest returns objects from the manifest after it has been filtered with selected and ignored resource expressions.
func filteredObjects(t test.Failer, manifest, selectResources, ignoreResources string) object.K8sObjects {
	objs, err := object.ParseK8sObjectsFromYAMLManifest(filteredManifest(t, manifest, selectResources, ignoreResources))
	if err != nil {
		t.Fatal(err)
	}
	return objs
}

func mustSelect(t test.Failer, selector map[string]string, labels map[string]string) {
	t.Helper()
	kselector := labels2.Set(selector).AsSelectorPreValidated()
	if !kselector.Matches(labels2.Set(labels)) {
		t.Fatalf("%v does not select %v", selector, labels)
	}
}

func mustNotSelect(t test.Failer, selector map[string]string, labels map[string]string) {
	t.Helper()
	kselector := labels2.Set(selector).AsSelectorPreValidated()
	if kselector.Matches(labels2.Set(labels)) {
		t.Fatalf("%v selects %v when it should not", selector, labels)
	}
}

func mustGetLabels(t test.Failer, obj object.K8sObject, path string) map[string]string {
	t.Helper()
	got := mustGetPath(t, obj, path)
	conv, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("could not convert %v", got)
	}
	ret := map[string]string{}
	for k, v := range conv {
		sv, ok := v.(string)
		if !ok {
			t.Fatalf("could not convert to string %v", v)
		}
		ret[k] = sv
	}
	return ret
}

func mustGetPath(t test.Failer, obj object.K8sObject, path string) interface{} {
	t.Helper()
	got, f, err := tpath.GetFromTreePath(obj.UnstructuredObject().UnstructuredContent(), util.PathFromString(path))
	if err != nil {
		t.Fatal(err)
	}
	if !f {
		t.Fatalf("couldn't find path %v", path)
	}
	return got
}

func mustFindObject(t test.Failer, objs object.K8sObjects, name, kind string) object.K8sObject {
	t.Helper()
	o := findObject(objs, name, kind)
	if o == nil {
		t.Fatalf("expected %v/%v", name, kind)
		return object.K8sObject{}
	}
	return *o
}

func findObject(objs object.K8sObjects, name, kind string) *object.K8sObject {
	for _, o := range objs {
		if o.Kind == kind && o.Name == name {
			return o
		}
	}
	return nil
}

func objectHashesOrdered(objs object.K8sObjects) []string {
	var out []string
	for _, o := range objs {
		out = append(out, o.Hash())
	}
	return out
}

func objectHashMap(objs object.K8sObjects) map[string]*object.K8sObject {
	out := make(map[string]*object.K8sObject)
	for _, o := range objs {
		out[o.Hash()] = o
	}
	return out
}

func createTempDirOrFail(t *testing.T, prefix string) string {
	dir, err := ioutil.TempDir("", prefix)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func removeDirOrFail(t *testing.T, path string) {
	err := os.RemoveAll(path)
	if err != nil {
		t.Fatal(err)
	}
}
