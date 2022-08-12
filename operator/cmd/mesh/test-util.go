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
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	labels2 "k8s.io/apimachinery/pkg/labels"

	name2 "istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pkg/test"
	"istio.io/pkg/log"
)

// PathValue is a path/value type.
type PathValue struct {
	path  string
	value any
}

// String implements the Stringer interface.
func (pv *PathValue) String() string {
	return fmt.Sprintf("%s:%v", pv.path, pv.value)
}

// ObjectSet is a set of objects maintained both as a slice (for ordering) and map (for speed).
type ObjectSet struct {
	objSlice object.K8sObjects
	objMap   map[string]*object.K8sObject
	keySlice []string
}

// NewObjectSet creates a new ObjectSet from objs and returns a pointer to it.
func NewObjectSet(objs object.K8sObjects) *ObjectSet {
	ret := &ObjectSet{}
	for _, o := range objs {
		ret.append(o)
	}
	return ret
}

// parseObjectSetFromManifest parses an ObjectSet from the given manifest.
func parseObjectSetFromManifest(manifest string) (*ObjectSet, error) {
	objSlice, err := object.ParseK8sObjectsFromYAMLManifest(manifest)
	return NewObjectSet(objSlice), err
}

// append appends an object to o.
func (o *ObjectSet) append(obj *object.K8sObject) {
	h := obj.Hash()
	o.objSlice = append(o.objSlice, obj)
	if o.objMap == nil {
		o.objMap = make(map[string]*object.K8sObject)
	}
	o.objMap[h] = obj
	o.keySlice = append(o.keySlice, h)
}

// size reports the length of o.
func (o *ObjectSet) size() int {
	return len(o.keySlice)
}

// nameMatches returns a subset of o where objects names match the given regex.
func (o *ObjectSet) nameMatches(nameRegex string) *ObjectSet {
	ret := &ObjectSet{}
	for k, v := range o.objMap {
		_, _, objName := object.FromHash(k)
		m, err := regexp.MatchString(nameRegex, objName)
		if err != nil {
			log.Error(err.Error())
			continue
		}
		if m {
			ret.append(v)
		}
	}
	return ret
}

// nameEquals returns the object in o whose name matches "name", or nil if no object name matches.
func (o *ObjectSet) nameEquals(name string) *object.K8sObject {
	for k, v := range o.objMap {
		_, _, objName := object.FromHash(k)
		if objName == name {
			return v
		}
	}
	return nil
}

// kind returns a subset of o where kind matches the given value.
func (o *ObjectSet) kind(kind string) *ObjectSet {
	ret := &ObjectSet{}
	for k, v := range o.objMap {
		objKind, _, _ := object.FromHash(k)
		if objKind == kind {
			ret.append(v)
		}
	}
	return ret
}

// namespace returns a subset of o where namespace matches the given value or fails if it's not found in objs.
func (o *ObjectSet) namespace(namespace string) *ObjectSet {
	ret := &ObjectSet{}
	for k, v := range o.objMap {
		_, objNamespace, _ := object.FromHash(k)
		if objNamespace == namespace {
			ret.append(v)
		}
	}
	return ret
}

// labels returns a subset of o where the object's labels match all the given labels.
func (o *ObjectSet) labels(labels ...string) *ObjectSet {
	ret := &ObjectSet{}
	for _, obj := range o.objMap {
		hasAll := true
		for _, l := range labels {
			lkv := strings.Split(l, "=")
			if len(lkv) != 2 {
				panic("label must have format key=value")
			}
			if !hasLabel(obj, lkv[0], lkv[1]) {
				hasAll = false
				break
			}
		}
		if hasAll {
			ret.append(obj)
		}
	}
	return ret
}

// HasLabel reports whether 0 has the given label.
func hasLabel(o *object.K8sObject, label, value string) bool {
	got, found, err := tpath.Find(o.UnstructuredObject().UnstructuredContent(), util.PathFromString("metadata.labels"))
	if err != nil {
		log.Errorf("bad path: %s", err)
		return false
	}
	if !found {
		return false
	}
	return got.(map[string]any)[label] == value
}

// mustGetService returns the service with the given name or fails if it's not found in objs.
func mustGetService(g *gomega.WithT, objs *ObjectSet, name string) *object.K8sObject {
	obj := objs.kind(name2.ServiceStr).nameEquals(name)
	g.Expect(obj).Should(gomega.Not(gomega.BeNil()))
	return obj
}

// mustGetDeployment returns the deployment with the given name or fails if it's not found in objs.
func mustGetDeployment(g *gomega.WithT, objs *ObjectSet, deploymentName string) *object.K8sObject {
	obj := objs.kind(name2.DeploymentStr).nameEquals(deploymentName)
	g.Expect(obj).Should(gomega.Not(gomega.BeNil()))
	return obj
}

// mustGetClusterRole returns the clusterRole with the given name or fails if it's not found in objs.
func mustGetClusterRole(g *gomega.WithT, objs *ObjectSet, name string) *object.K8sObject {
	obj := objs.kind(name2.ClusterRoleStr).nameEquals(name)
	g.Expect(obj).Should(gomega.Not(gomega.BeNil()))
	return obj
}

// mustGetRole returns the role with the given name or fails if it's not found in objs.
func mustGetRole(g *gomega.WithT, objs *ObjectSet, name string) *object.K8sObject {
	obj := objs.kind(name2.RoleStr).nameEquals(name)
	g.Expect(obj).Should(gomega.Not(gomega.BeNil()))
	return obj
}

// mustGetContainer returns the container tree with the given name in the deployment with the given name.
func mustGetContainer(g *gomega.WithT, objs *ObjectSet, deploymentName, containerName string) map[string]any {
	obj := mustGetDeployment(g, objs, deploymentName)
	container := obj.Container(containerName)
	g.Expect(container).Should(gomega.Not(gomega.BeNil()), fmt.Sprintf("Expected to get container %s in deployment %s", containerName, deploymentName))
	return container
}

// mustGetEndpoint returns the endpoint tree with the given name in the deployment with the given name.
func mustGetEndpoint(g *gomega.WithT, objs *ObjectSet, endpointName string) *object.K8sObject {
	obj := objs.kind(name2.EndpointStr).nameEquals(endpointName)
	if obj == nil {
		return nil
	}
	g.Expect(obj).Should(gomega.Not(gomega.BeNil()))
	return obj
}

// mustGetMutatingWebhookConfiguration returns the mutatingWebhookConfiguration with the given name or fails if it's not found in objs.
func mustGetMutatingWebhookConfiguration(g *gomega.WithT, objs *ObjectSet, mutatingWebhookConfigurationName string) *object.K8sObject {
	obj := objs.kind(name2.MutatingWebhookConfigurationStr).nameEquals(mutatingWebhookConfigurationName)
	g.Expect(obj).Should(gomega.Not(gomega.BeNil()))
	return obj
}

// HavePathValueEqual matches map[string]interface{} tree against a PathValue.
func HavePathValueEqual(expected any) types.GomegaMatcher {
	return &HavePathValueEqualMatcher{
		expected: expected,
	}
}

// HavePathValueEqualMatcher is a matcher type for HavePathValueEqual.
type HavePathValueEqualMatcher struct {
	expected any
}

// Match implements the Matcher interface.
func (m *HavePathValueEqualMatcher) Match(actual any) (bool, error) {
	pv := m.expected.(PathValue)
	node := actual.(map[string]any)
	got, f, err := tpath.GetPathContext(node, util.PathFromString(pv.path), false)
	if err != nil || !f {
		return false, err
	}
	if reflect.TypeOf(got.Node) != reflect.TypeOf(pv.value) {
		return false, fmt.Errorf("comparison types don't match: got %v(%T), want %v(%T)", got.Node, got.Node, pv.value, pv.value)
	}
	if !reflect.DeepEqual(got.Node, pv.value) {
		return false, fmt.Errorf("values don't match: got %v, want %v", got.Node, pv.value)
	}
	return true, nil
}

// FailureMessage implements the Matcher interface.
func (m *HavePathValueEqualMatcher) FailureMessage(actual any) string {
	pv := m.expected.(PathValue)
	node := actual.(map[string]any)
	return fmt.Sprintf("Expected the following parseObjectSetFromManifest to have path=value %s=%v\n\n%v", pv.path, pv.value, util.ToYAML(node))
}

// NegatedFailureMessage implements the Matcher interface.
func (m *HavePathValueEqualMatcher) NegatedFailureMessage(actual any) string {
	pv := m.expected.(PathValue)
	node := actual.(map[string]any)
	return fmt.Sprintf("Expected the following parseObjectSetFromManifest not to have path=value %s=%v\n\n%v", pv.path, pv.value, util.ToYAML(node))
}

// HavePathValueMatchRegex matches map[string]interface{} tree against a PathValue.
func HavePathValueMatchRegex(expected any) types.GomegaMatcher {
	return &HavePathValueMatchRegexMatcher{
		expected: expected,
	}
}

// HavePathValueMatchRegexMatcher is a matcher type for HavePathValueMatchRegex.
type HavePathValueMatchRegexMatcher struct {
	expected any
}

// Match implements the Matcher interface.
func (m *HavePathValueMatchRegexMatcher) Match(actual any) (bool, error) {
	pv := m.expected.(PathValue)
	node := actual.(map[string]any)
	got, f, err := tpath.GetPathContext(node, util.PathFromString(pv.path), false)
	if err != nil || !f {
		return false, err
	}
	if reflect.TypeOf(got.Node).Kind() != reflect.String || reflect.TypeOf(pv.value).Kind() != reflect.String {
		return false, fmt.Errorf("comparison types must both be string: got %v(%T), want %v(%T)", got.Node, got.Node, pv.value, pv.value)
	}
	gotS := got.Node.(string)
	wantS := pv.value.(string)
	ok, err := regexp.MatchString(wantS, gotS)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("values don't match: got %v, want %v", got.Node, pv.value)
	}
	return true, nil
}

// FailureMessage implements the Matcher interface.
func (m *HavePathValueMatchRegexMatcher) FailureMessage(actual any) string {
	pv := m.expected.(PathValue)
	node := actual.(map[string]any)
	return fmt.Sprintf("Expected the following parseObjectSetFromManifest to regex match path=value %s=%v\n\n%v", pv.path, pv.value, util.ToYAML(node))
}

// NegatedFailureMessage implements the Matcher interface.
func (m *HavePathValueMatchRegexMatcher) NegatedFailureMessage(actual any) string {
	pv := m.expected.(PathValue)
	node := actual.(map[string]any)
	return fmt.Sprintf("Expected the following parseObjectSetFromManifest not to regex match path=value %s=%v\n\n%v", pv.path, pv.value, util.ToYAML(node))
}

// HavePathValueContain matches map[string]interface{} tree against a PathValue.
func HavePathValueContain(expected any) types.GomegaMatcher {
	return &HavePathValueContainMatcher{
		expected: expected,
	}
}

// HavePathValueContainMatcher is a matcher type for HavePathValueContain.
type HavePathValueContainMatcher struct {
	expected any
}

// Match implements the Matcher interface.
func (m *HavePathValueContainMatcher) Match(actual any) (bool, error) {
	pv := m.expected.(PathValue)
	node := actual.(map[string]any)
	got, f, err := tpath.GetPathContext(node, util.PathFromString(pv.path), false)
	if err != nil || !f {
		return false, err
	}
	if reflect.TypeOf(got.Node) != reflect.TypeOf(pv.value) {
		return false, fmt.Errorf("comparison types don't match: got %T, want %T", got.Node, pv.value)
	}
	gotValStr := util.ToYAML(got.Node)
	subsetValStr := util.ToYAML(pv.value)
	overlay, err := util.OverlayYAML(gotValStr, subsetValStr)
	if err != nil {
		return false, err
	}
	if overlay != gotValStr {
		return false, fmt.Errorf("actual value:\n\n%s\ndoesn't contain expected subset:\n\n%s", gotValStr, subsetValStr)
	}
	return true, nil
}

// FailureMessage implements the Matcher interface.
func (m *HavePathValueContainMatcher) FailureMessage(actual any) string {
	pv := m.expected.(PathValue)
	node := actual.(map[string]any)
	return fmt.Sprintf("Expected path %s with value \n\n%v\nto be a subset of \n\n%v", pv.path, pv.value, util.ToYAML(node))
}

// NegatedFailureMessage implements the Matcher interface.
func (m *HavePathValueContainMatcher) NegatedFailureMessage(actual any) string {
	pv := m.expected.(PathValue)
	node := actual.(map[string]any)
	return fmt.Sprintf("Expected path %s with value \n\n%v\nto NOT be a subset of \n\n%v", pv.path, pv.value, util.ToYAML(node))
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
	conv, ok := got.(map[string]any)
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

func mustGetPath(t test.Failer, obj object.K8sObject, path string) any {
	t.Helper()
	got, f, err := tpath.Find(obj.UnstructuredObject().UnstructuredContent(), util.PathFromString(path))
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

// mustGetValueAtPath returns the value at the given path in the unstructured tree t. Fails if the path is not found
// in the tree.
func mustGetValueAtPath(g *gomega.WithT, t map[string]any, path string) any {
	got, f, err := tpath.GetPathContext(t, util.PathFromString(path), false)
	g.Expect(err).Should(gomega.BeNil(), "path %s should exist (%s)", path, err)
	g.Expect(f).Should(gomega.BeTrue(), "path %s should exist", path)
	return got.Node
}

func createTempDirOrFail(t *testing.T, prefix string) string {
	dir, err := os.MkdirTemp("", prefix)
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

// toMap transforms a comma separated key:value list (e.g. "a:aval, b:bval") to a map.
func toMap(s string) map[string]any {
	out := make(map[string]any)
	for _, l := range strings.Split(s, ",") {
		l = strings.TrimSpace(l)
		kv := strings.Split(l, ":")
		if len(kv) != 2 {
			panic("bad key:value in " + s)
		}
		out[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// endpointSubsetAddressVal returns a map having subset address type for an endpint.
func endpointSubsetAddressVal(hostname, ip, nodeName string) map[string]any {
	out := make(map[string]any)
	if hostname != "" {
		out["hostname"] = hostname
	}
	if ip != "" {
		out["ip"] = ip
	}
	if nodeName != "" {
		out["nodeName"] = nodeName
	}
	return out
}

// portVal returns a map having service port type. A value of -1 for port or targetPort leaves those keys unset.
func portVal(name string, port, targetPort int64) map[string]any {
	out := make(map[string]any)
	if name != "" {
		out["name"] = name
	}
	if port != -1 {
		out["port"] = port
	}
	if targetPort != -1 {
		out["targetPort"] = targetPort
	}
	return out
}

// checkRoleBindingsReferenceRoles fails if any RoleBinding in objs references a Role that isn't found in objs.
func checkRoleBindingsReferenceRoles(g *gomega.WithT, objs *ObjectSet) {
	for _, o := range objs.kind(name2.RoleBindingStr).objSlice {
		ou := o.Unstructured()
		rrname := mustGetValueAtPath(g, ou, "roleRef.name")
		mustGetRole(g, objs, rrname.(string))
	}
}

// checkClusterRoleBindingsReferenceRoles fails if any RoleBinding in objs references a Role that isn't found in objs.
func checkClusterRoleBindingsReferenceRoles(g *gomega.WithT, objs *ObjectSet) {
	for _, o := range objs.kind(name2.ClusterRoleBindingStr).objSlice {
		ou := o.Unstructured()
		rrname := mustGetValueAtPath(g, ou, "roleRef.name")
		mustGetClusterRole(g, objs, rrname.(string))
	}
}
