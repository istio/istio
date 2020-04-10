package mesh

import (
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"regexp"
	"testing"

	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	labels2 "k8s.io/apimachinery/pkg/labels"

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

func parseObjectSetFromManifest(t test.Failer, manifest string) *objectSet {
	ret := &objectSet{}
	var err error
	ret.objSlice, err = object.ParseK8sObjectsFromYAMLManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range ret.objSlice {
		ret.append(o)
	}
	return ret
}

// objectSet is a set of objects maintained both as a slice (for ordering) and map (for speed).
type objectSet struct {
	objSlice object.K8sObjects
	objMap   map[string]*object.K8sObject
	keySlice []string
}

// append appends an object to o.
func (o *objectSet) append(obj *object.K8sObject) {
	h := obj.Hash()
	o.objSlice = append(o.objSlice, obj)
	if o.objMap == nil {
		o.objMap = make(map[string]*object.K8sObject)
	}
	o.objMap[h] = obj
	o.keySlice = append(o.keySlice, h)
}

// size reports the length of o.
func (o *objectSet) size() int {
	return len(o.keySlice)
}

// nameMatches returns a subset of o where objects names match the given regex.
func (o *objectSet) nameMatches(nameRegex string) *objectSet {
	ret := &objectSet{}
	for k, v := range o.objMap {
		_, _, objName := object.FromHash(k)
		m, err := regexp.MatchString(nameRegex, objName)
		if err != nil && m {
			ret.append(v)
		}
	}
	return ret
}

// nameEquals returns the object in o whose name matches "name", or nil if no object name matches.
func (o *objectSet) nameEquals(name string) *object.K8sObject {
	for k, v := range o.objMap {
		_, _, objName := object.FromHash(k)
		if objName == name {
			return v
		}
	}
	return nil
}

// kind returns a subset of o where kind matches the given value.
func (o *objectSet) kind(kind string) *objectSet {
	ret := &objectSet{}
	for k, v := range o.objMap {
		objKind, _, _ := object.FromHash(k)
		if objKind == kind {
			ret.append(v)
		}
	}
	return ret
}

// namespace returns a subset of o where namespace matches the given value.
func (o *objectSet) namespace(namespace string) *objectSet {
	ret := &objectSet{}
	for k, v := range o.objMap {
		_, objNamespace, _ := object.FromHash(k)
		if objNamespace == namespace {
			ret.append(v)
		}
	}
	return ret
}

// mustGetContainer returns the container tree with the given name in the deployment with the given name.
// The function fails through g if no matching container is found.
func mustGetContainer(g *gomega.WithT, objs *objectSet, deploymentName, containerName string) map[string]interface{} {
	obj := objs.kind("Deployment").nameEquals(deploymentName)
	g.Expect(obj).Should(Not(BeNil()))
	container := obj.Container(containerName)
	g.Expect(container).Should(Not(BeNil()))
	return container
}

// HavePathValueEqual matches map[string]interface{} tree against a PathValue.
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
	pv := m.expected.(PathValue)
	node := actual.(map[string]interface{})
	got, f, err := tpath.GetPathContext(node, util.PathFromString(pv.path))
	if err != nil || !f {
		return false, err
	}
	if !reflect.DeepEqual(got.Node, pv.value) {
		return false, nil
	}
	return true, nil
}

// FailureMessage implements the Matcher interface.
func (m *HavePathValueEqualMatcher) FailureMessage(actual interface{}) string {
	pv := m.expected.(PathValue)
	node := actual.(map[string]interface{})
	return fmt.Sprintf("Expected the following parseObjectSetFromManifest to have path=value %s=%v\n\n%v", pv.path, pv.value, util.ToYAML(node))
}

// NegatedFailureMessage implements the Matcher interface.
func (m *HavePathValueEqualMatcher) NegatedFailureMessage(actual interface{}) string {
	pv := m.expected.(PathValue)
	node := actual.(map[string]interface{})
	return fmt.Sprintf("Expected the following parseObjectSetFromManifest not to have path=value %s=%v\n\n%v", pv.path, pv.value, util.ToYAML(node))
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
