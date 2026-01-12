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
	"reflect"
	"regexp"
	"strings"

	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	labels2 "k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/values"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/yml"
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
	objSlice []manifest.Manifest
	objMap   map[string]manifest.Manifest
	keySlice []string
}

// NewObjectSet creates a new ObjectSet from objs and returns a pointer to it.
func NewObjectSet(objs []manifest.Manifest) *ObjectSet {
	ret := &ObjectSet{}
	for _, o := range objs {
		ret.append(o)
	}
	return ret
}

// parseObjectSetFromManifest parses an ObjectSet from the given manifest.
func parseObjectSetFromManifest(t test.Failer, raw string) *ObjectSet {
	spl := yml.SplitString(raw)
	mfs, err := manifest.Parse(spl)
	if err != nil {
		t.Fatal(err)
	}
	return NewObjectSet(mfs)
}

// append appends an object to o.
func (o *ObjectSet) append(obj manifest.Manifest) {
	h := obj.Hash()
	o.objSlice = append(o.objSlice, obj)
	if o.objMap == nil {
		o.objMap = make(map[string]manifest.Manifest)
	}
	o.objMap[h] = obj
	o.keySlice = append(o.keySlice, h)
}

// size reports the length of o.
func (o *ObjectSet) size() int {
	return len(o.keySlice)
}

// FromHash parses kind, namespace and name from a hash.
func FromHash(hash string) (kind, namespace, name string) {
	hv := strings.Split(hash, ":")
	if len(hv) != 3 {
		return "Bad hash string: " + hash, "", ""
	}
	kind, namespace, name = hv[0], hv[1], hv[2]
	return kind, namespace, name
}

// nameMatches returns a subset of o where objects names match the given regex.
func (o *ObjectSet) nameMatches(nameRegex string) *ObjectSet {
	ret := &ObjectSet{}
	for k, v := range o.objMap {
		_, _, objName := FromHash(k)
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
func (o *ObjectSet) nameEquals(name string) *manifest.Manifest {
	for k, v := range o.objMap {
		_, _, objName := FromHash(k)
		if objName == name {
			return &v
		}
	}
	return nil
}

// kind returns a subset of o where kind matches the given value.
func (o *ObjectSet) kind(kind string) *ObjectSet {
	ret := &ObjectSet{}
	for k, v := range o.objMap {
		objKind, _, _ := FromHash(k)
		if objKind == kind {
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
func hasLabel(o manifest.Manifest, label, value string) bool {
	m := values.TryGetPathAs[map[string]any](o.Object, "metadata.labels")
	return m[label] == value
}

// mustGetService returns the service with the given name or fails if it's not found in objs.
func mustGetService(g *WithT, objs *ObjectSet, name string) manifest.Manifest {
	obj := objs.kind(gvk.Service.Kind).nameEquals(name)
	g.Expect(obj).Should(Not(BeNil()))
	return *obj
}

// mustGetDeployment returns the deployment with the given name or fails if it's not found in objs.
func mustGetDeployment(g *WithT, objs *ObjectSet, deploymentName string) manifest.Manifest {
	obj := objs.kind(gvk.Deployment.Kind).nameEquals(deploymentName)
	g.Expect(obj).Should(Not(BeNil()))
	return *obj
}

// mustGetDaemonset returns the DaemonSet with the given name or fails if it's not found in objs.
func mustGetDaemonset(g *WithT, objs *ObjectSet, daemonSetName string) manifest.Manifest {
	obj := objs.kind(gvk.DaemonSet.Kind).nameEquals(daemonSetName)
	g.Expect(obj).Should(Not(BeNil()))
	return *obj
}

// mustGetRole returns the role with the given name or fails if it's not found in objs.
// nolint: unparam
func mustGetRole(g *WithT, objs *ObjectSet, name string) manifest.Manifest {
	obj := objs.kind(manifest.Role).nameEquals(name)
	g.Expect(obj).Should(Not(BeNil()))
	return *obj
}

// mustGetContainer returns the container tree with the given name in the deployment with the given name.
func mustGetContainer(g *WithT, objs *ObjectSet, deploymentName, containerName string) map[string]any {
	obj := mustGetDeployment(g, objs, deploymentName)
	container := getContainer(obj, containerName)
	g.Expect(container).Should(Not(BeNil()), fmt.Sprintf("Expected to get container %s in deployment %s", containerName, deploymentName))
	return container
}

func getContainer(obj manifest.Manifest, containerName string) map[string]any {
	var container map[string]any
	sl, ok := values.Map(obj.Object).GetPath("spec.template.spec.containers")
	if ok {
		for _, cm := range sl.([]any) {
			t := cm.(map[string]any)
			if t["name"] == containerName {
				container = t
				break
			}
		}
	}
	return container
}

// mustGetContainer returns the container tree with the given name in the deployment with the given name.
func mustGetContainerFromDaemonset(g *WithT, objs *ObjectSet, daemonSetName, containerName string) map[string]any {
	obj := mustGetDaemonset(g, objs, daemonSetName)
	container := getContainer(obj, containerName)
	g.Expect(container).Should(Not(BeNil()), fmt.Sprintf("Expected to get container %s in daemonset %s", containerName, daemonSetName))
	return container
}

// mustGetEndpointSlice returns the EndpointSlice with the given name or fails if it's not found in objs.
func mustGetEndpointSlice(g *WithT, objs *ObjectSet, endpointSliceName string) *manifest.Manifest {
	obj := objs.kind(gvk.EndpointSlice.Kind).nameEquals(endpointSliceName)
	g.Expect(obj).Should(Not(BeNil()), fmt.Sprintf("Expected to get EndpointSlice %s", endpointSliceName))
	return obj
}

// mustGetMutatingWebhookConfiguration returns the mutatingWebhookConfiguration with the given name or fails if it's not found in objs.
func mustGetMutatingWebhookConfiguration(g *WithT, objs *ObjectSet, mutatingWebhookConfigurationName string) *manifest.Manifest {
	obj := objs.kind(gvk.MutatingWebhookConfiguration.Kind).nameEquals(mutatingWebhookConfigurationName)
	g.Expect(obj).Should(Not(BeNil()))
	return obj
}

func mustGetValidatingWebhookConfiguration(g *WithT, objs *ObjectSet, cName string) *manifest.Manifest {
	obj := objs.kind(gvk.ValidatingWebhookConfiguration.Kind).nameEquals(cName)
	g.Expect(obj).Should(Not(BeNil()))
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
	got, f := values.MustCastAsMap(actual).GetPath(pv.path)
	if !f {
		return false, fmt.Errorf("could not find path %v", pv.path)
	}
	if reflect.TypeOf(got) != reflect.TypeOf(pv.value) {
		return false, fmt.Errorf("comparison types don't match: got %v(%T), want %v(%T)", got, got, pv.value, pv.value)
	}
	if !reflect.DeepEqual(got, pv.value) {
		return false, fmt.Errorf("values don't match: got %v, want %v", got, pv.value)
	}
	return true, nil
}

// FailureMessage implements the Matcher interface.
func (m *HavePathValueEqualMatcher) FailureMessage(actual any) string {
	pv := m.expected.(PathValue)
	node := actual.(map[string]any)
	return fmt.Sprintf("Expected the following parseObjectSetFromManifest to have path=value %s=%v\n\n%v", pv.path, pv.value, toYAML(node))
}

// NegatedFailureMessage implements the Matcher interface.
func (m *HavePathValueEqualMatcher) NegatedFailureMessage(actual any) string {
	pv := m.expected.(PathValue)
	node := actual.(map[string]any)
	return fmt.Sprintf("Expected the following parseObjectSetFromManifest not to have path=value %s=%v\n\n%v", pv.path, pv.value, toYAML(node))
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
	got, f := values.MustCastAsMap(actual).GetPath(pv.path)
	if !f {
		return false, fmt.Errorf("could not find path %v", pv.path)
	}
	if reflect.TypeOf(got).Kind() != reflect.String || reflect.TypeOf(pv.value).Kind() != reflect.String {
		return false, fmt.Errorf("comparison types must both be string: got %v(%T), want %v(%T)", got, got, pv.value, pv.value)
	}
	gotS := got.(string)
	wantS := pv.value.(string)
	ok, err := regexp.MatchString(wantS, gotS)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("values don't match: got %v, want %v", got, pv.value)
	}
	return true, nil
}

// FailureMessage implements the Matcher interface.
func (m *HavePathValueMatchRegexMatcher) FailureMessage(actual any) string {
	pv := m.expected.(PathValue)
	node := actual.(map[string]any)
	return fmt.Sprintf("Expected the following parseObjectSetFromManifest to regex match path=value %s=%v\n\n%v", pv.path, pv.value, toYAML(node))
}

// NegatedFailureMessage implements the Matcher interface.
func (m *HavePathValueMatchRegexMatcher) NegatedFailureMessage(actual any) string {
	pv := m.expected.(PathValue)
	node := actual.(map[string]any)
	return fmt.Sprintf("Expected the following parseObjectSetFromManifest not to regex match path=value %s=%v\n\n%v", pv.path, pv.value, toYAML(node))
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
	got, f := values.MustCastAsMap(actual).GetPath(pv.path)
	if !f {
		return false, fmt.Errorf("could not find path %v", pv.path)
	}
	if reflect.TypeOf(got) != reflect.TypeOf(pv.value) {
		return false, fmt.Errorf("comparison types don't match: got %T, want %T", got, pv.value)
	}
	g := got.(map[string]any)
	want := pv.value.(map[string]any)
	for k, v := range want {
		gv := g[k]
		if !reflect.DeepEqual(gv, v) {
			return false, fmt.Errorf("values don't match %q: got %v, want %v", k, gv, v)
		}
	}
	return true, nil
}

// FailureMessage implements the Matcher interface.
func (m *HavePathValueContainMatcher) FailureMessage(actual any) string {
	pv := m.expected.(PathValue)
	node := actual.(map[string]any)
	return fmt.Sprintf("Expected path %s with value \n\n%v\nto be a subset of \n\n%v", pv.path, pv.value, toYAML(node))
}

// NegatedFailureMessage implements the Matcher interface.
func (m *HavePathValueContainMatcher) NegatedFailureMessage(actual any) string {
	pv := m.expected.(PathValue)
	node := actual.(map[string]any)
	return fmt.Sprintf("Expected path %s with value \n\n%v\nto NOT be a subset of \n\n%v", pv.path, pv.value, toYAML(node))
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

func mustGetLabels(t test.Failer, obj manifest.Manifest, path string) map[string]string {
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

func mustGetPath(t test.Failer, obj manifest.Manifest, path string) any {
	t.Helper()
	v, f := values.Map(obj.Object).GetPath(path)
	if !f {
		t.Fatalf("couldn't find path %v", path)
	}
	return v
}

func mustFindObject(t test.Failer, objs []manifest.Manifest, name, kind string) manifest.Manifest {
	t.Helper()
	o := findObject(objs, name, kind)
	if o == nil {
		t.Fatalf("expected %v/%v", name, kind)
		return manifest.Manifest{}
	}
	return *o
}

func findObject(objs []manifest.Manifest, name, kind string) *manifest.Manifest {
	for _, o := range objs {
		if o.GroupVersionKind().Kind == kind && o.GetName() == name {
			return &o
		}
	}
	return nil
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
func checkRoleBindingsReferenceRoles(g *WithT, objs *ObjectSet) {
	for _, o := range objs.kind(manifest.RoleBinding).objSlice {
		rrname := values.Map(o.Object).GetPathString("roleRef.name")
		_ = mustGetRole(g, objs, rrname)
	}
}

// checkClusterRoleBindingsReferenceRoles fails if any RoleBinding in objs references a Role that isn't found in objs.
func checkClusterRoleBindingsReferenceRoles(g *WithT, objs *ObjectSet) {
	for _, o := range objs.kind(manifest.ClusterRoleBinding).objSlice {
		rrname := values.Map(o.Object).GetPathString("roleRef.name")
		_ = mustGetRole(g, objs, rrname)
	}
}

// toYAML returns a YAML string representation of val, or the error string if an error occurs.
func toYAML(val any) string {
	y, err := yaml.Marshal(val)
	if err != nil {
		return err.Error()
	}
	return string(y)
}
