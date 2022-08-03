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

/*
Package manifest provides functions for going between in-memory k8s objects (unstructured.Unstructured) and their JSON
or YAML representations.
*/
package object

import (
	"bufio"
	"bytes"
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/helm"
	names "istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/pkg/log"
)

const (
	// YAMLSeparator is a separator for multi-document YAML files.
	YAMLSeparator = "\n---\n"
)

// K8sObject is an in-memory representation of a k8s object, used for moving between different representations
// (Unstructured, JSON, YAML) with cached rendering.
type K8sObject struct {
	object *unstructured.Unstructured

	Group     string
	Kind      string
	Name      string
	Namespace string

	json []byte
	yaml []byte
}

// NewK8sObject creates a new K8sObject and returns a ptr to it.
func NewK8sObject(u *unstructured.Unstructured, json, yaml []byte) *K8sObject {
	o := &K8sObject{
		object: u,
		json:   json,
		yaml:   yaml,
	}

	gvk := u.GetObjectKind().GroupVersionKind()
	o.Group = gvk.Group
	o.Kind = gvk.Kind
	o.Name = u.GetName()
	o.Namespace = u.GetNamespace()

	return o
}

// Hash returns a unique, insecure hash based on kind, namespace and name.
func Hash(kind, namespace, name string) string {
	switch kind {
	case names.ClusterRoleStr, names.ClusterRoleBindingStr, names.MeshPolicyStr:
		namespace = ""
	}
	return strings.Join([]string{kind, namespace, name}, ":")
}

// FromHash parses kind, namespace and name from a hash.
func FromHash(hash string) (kind, namespace, name string) {
	hv := strings.Split(hash, ":")
	if len(hv) != 3 {
		return "Bad hash string: " + hash, "", ""
	}
	kind, namespace, name = hv[0], hv[1], hv[2]
	return
}

// HashNameKind returns a unique, insecure hash based on kind and name.
func HashNameKind(kind, name string) string {
	return strings.Join([]string{kind, name}, ":")
}

// ParseJSONToK8sObject parses JSON to an K8sObject.
func ParseJSONToK8sObject(json []byte) (*K8sObject, error) {
	o, _, err := unstructured.UnstructuredJSONScheme.Decode(json, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("error parsing json into unstructured object: %v", err)
	}

	u, ok := o.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("parsed unexpected type %T", o)
	}

	return NewK8sObject(u, json, nil), nil
}

// ParseYAMLToK8sObject parses YAML to an Object.
func ParseYAMLToK8sObject(yaml []byte) (*K8sObject, error) {
	r := bytes.NewReader(yaml)
	decoder := k8syaml.NewYAMLOrJSONDecoder(r, 1024)

	out := &unstructured.Unstructured{}
	err := decoder.Decode(out)
	if err != nil {
		return nil, fmt.Errorf("error decoding object %v: %v", string(yaml), err)
	}
	return NewK8sObject(out, nil, yaml), nil
}

// UnstructuredObject exposes the raw object, primarily for testing
func (o *K8sObject) UnstructuredObject() *unstructured.Unstructured {
	return o.object
}

// ResolveK8sConflict - This method resolves k8s object possible
// conflicting settings. Which K8sObjects may need such method
// depends on the type of the K8sObject.
func (o *K8sObject) ResolveK8sConflict() *K8sObject {
	if o.Kind == names.PDBStr {
		return resolvePDBConflict(o)
	}
	return o
}

// Unstructured exposes the raw object content, primarily for testing
func (o *K8sObject) Unstructured() map[string]any {
	return o.UnstructuredObject().UnstructuredContent()
}

// Container returns a container subtree for Deployment objects if one is found, or nil otherwise.
func (o *K8sObject) Container(name string) map[string]any {
	u := o.Unstructured()
	path := fmt.Sprintf("spec.template.spec.containers.[name:%s]", name)
	node, f, err := tpath.GetPathContext(u, util.PathFromString(path), false)
	if err == nil && f {
		// Must be the type from the schema.
		return node.Node.(map[string]any)
	}
	return nil
}

// GroupVersionKind returns the GroupVersionKind for the K8sObject
func (o *K8sObject) GroupVersionKind() schema.GroupVersionKind {
	return o.object.GroupVersionKind()
}

// Version returns the APIVersion of the K8sObject
func (o *K8sObject) Version() string {
	return o.object.GetAPIVersion()
}

// Hash returns a unique hash for the K8sObject
func (o *K8sObject) Hash() string {
	return Hash(o.Kind, o.Namespace, o.Name)
}

// HashNameKind returns a hash for the K8sObject based on the name and kind only.
func (o *K8sObject) HashNameKind() string {
	return HashNameKind(o.Kind, o.Name)
}

// JSON returns a JSON representation of the K8sObject, using an internal cache.
func (o *K8sObject) JSON() ([]byte, error) {
	if o.json != nil {
		return o.json, nil
	}

	b, err := o.object.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return b, nil
}

// YAML returns a YAML representation of the K8sObject, using an internal cache.
func (o *K8sObject) YAML() ([]byte, error) {
	if o == nil {
		return nil, nil
	}
	if o.yaml != nil {
		return o.yaml, nil
	}
	oj, err := o.JSON()
	if err != nil {
		return nil, err
	}
	o.json = oj
	y, err := yaml.JSONToYAML(oj)
	if err != nil {
		return nil, err
	}
	o.yaml = y
	return y, nil
}

// YAMLDebugString returns a YAML representation of the K8sObject, or an error string if the K8sObject cannot be rendered to YAML.
func (o *K8sObject) YAMLDebugString() string {
	y, err := o.YAML()
	if err != nil {
		return err.Error()
	}
	return string(y)
}

// K8sObjects holds a collection of k8s objects, so that we can filter / sequence them
type K8sObjects []*K8sObject

// String implements the Stringer interface.
func (os K8sObjects) String() string {
	var out []string
	for _, oo := range os {
		out = append(out, oo.YAMLDebugString())
	}
	return strings.Join(out, helm.YAMLSeparator)
}

// Keys returns a slice with the keys of os.
func (os K8sObjects) Keys() []string {
	var out []string
	for _, oo := range os {
		out = append(out, oo.Hash())
	}
	return out
}

// UnstructuredItems returns the list of items of unstructured.Unstructured.
func (os K8sObjects) UnstructuredItems() []unstructured.Unstructured {
	var usList []unstructured.Unstructured
	for _, obj := range os {
		usList = append(usList, *obj.UnstructuredObject())
	}
	return usList
}

// ParseK8sObjectsFromYAMLManifest returns a K8sObjects representation of manifest.
func ParseK8sObjectsFromYAMLManifest(manifest string) (K8sObjects, error) {
	return ParseK8sObjectsFromYAMLManifestFailOption(manifest, true)
}

// ParseK8sObjectsFromYAMLManifestFailOption returns a K8sObjects representation of manifest. Continues parsing when a bad object
// is found if failOnError is set to false.
func ParseK8sObjectsFromYAMLManifestFailOption(manifest string, failOnError bool) (K8sObjects, error) {
	var b bytes.Buffer

	var yamls []string
	scanner := bufio.NewScanner(strings.NewReader(manifest))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "---") {
			// yaml separator
			yamls = append(yamls, b.String())
			b.Reset()
		} else {
			if _, err := b.WriteString(line); err != nil {
				return nil, err
			}
			if _, err := b.WriteString("\n"); err != nil {
				return nil, err
			}
		}
	}
	yamls = append(yamls, b.String())

	var objects K8sObjects

	for _, yaml := range yamls {
		yaml = removeNonYAMLLines(yaml)
		if yaml == "" {
			continue
		}
		o, err := ParseYAMLToK8sObject([]byte(yaml))
		if err != nil {
			e := fmt.Errorf("failed to parse YAML to a k8s object: %s", err)
			if failOnError {
				return nil, e
			}
			log.Error(err.Error())
			continue
		}
		if o.Valid() {
			objects = append(objects, o)
		}
	}

	return objects, nil
}

func removeNonYAMLLines(yms string) string {
	var b strings.Builder
	for _, s := range strings.Split(yms, "\n") {
		if strings.HasPrefix(s, "#") {
			continue
		}
		b.WriteString(s)
		b.WriteString("\n")
	}

	// helm charts sometimes emits blank objects with just a "disabled" comment.
	return strings.TrimSpace(b.String())
}

// YAMLManifest returns a YAML representation of K8sObjects os.
func (os K8sObjects) YAMLManifest() (string, error) {
	var b bytes.Buffer

	for i, item := range os {
		if i != 0 {
			if _, err := b.WriteString("\n\n"); err != nil {
				return "", err
			}
		}
		ym, err := item.YAML()
		if err != nil {
			return "", fmt.Errorf("error building yaml: %v", err)
		}
		if _, err := b.Write(ym); err != nil {
			return "", err
		}
		if _, err := b.Write([]byte(YAMLSeparator)); err != nil {
			return "", err
		}

	}

	return b.String(), nil
}

// Sort will order the items in K8sObjects in order of score, group, kind, name.  The intent is to
// have a deterministic ordering in which K8sObjects are applied.
func (os K8sObjects) Sort(score func(o *K8sObject) int) {
	sort.Slice(os, func(i, j int) bool {
		iScore := score(os[i])
		jScore := score(os[j])
		return iScore < jScore ||
			(iScore == jScore &&
				os[i].Group < os[j].Group) ||
			(iScore == jScore &&
				os[i].Group == os[j].Group &&
				os[i].Kind < os[j].Kind) ||
			(iScore == jScore &&
				os[i].Group == os[j].Group &&
				os[i].Kind == os[j].Kind &&
				os[i].Name < os[j].Name)
	})
}

// ToMap returns a map of K8sObject hash to K8sObject.
func (os K8sObjects) ToMap() map[string]*K8sObject {
	ret := make(map[string]*K8sObject)
	for _, oo := range os {
		if oo.Valid() {
			ret[oo.Hash()] = oo
		}
	}
	return ret
}

// ToNameKindMap returns a map of K8sObject name/kind hash to K8sObject.
func (os K8sObjects) ToNameKindMap() map[string]*K8sObject {
	ret := make(map[string]*K8sObject)
	for _, oo := range os {
		if oo.Valid() {
			ret[oo.HashNameKind()] = oo
		}
	}
	return ret
}

// Valid checks returns true if Kind of K8sObject is not empty.
func (o *K8sObject) Valid() bool {
	return o.Kind != ""
}

// FullName returns namespace/name of K8s object
func (o *K8sObject) FullName() string {
	return fmt.Sprintf("%s/%s", o.Namespace, o.Name)
}

// Equal returns true if o and other are both valid and equal to each other.
func (o *K8sObject) Equal(other *K8sObject) bool {
	if o == nil {
		return other == nil
	}
	if other == nil {
		return o == nil
	}

	ay, err := o.YAML()
	if err != nil {
		return false
	}
	by, err := other.YAML()
	if err != nil {
		return false
	}

	return util.IsYAMLEqual(string(ay), string(by))
}

func istioCustomResources(group string) bool {
	switch group {
	case names.ConfigAPIGroupName,
		names.SecurityAPIGroupName,
		names.AuthenticationAPIGroupName,
		names.NetworkingAPIGroupName:
		return true
	}
	return false
}

// DefaultObjectOrder is default sorting function used to sort k8s objects.
func DefaultObjectOrder() func(o *K8sObject) int {
	return func(o *K8sObject) int {
		gk := o.Group + "/" + o.Kind
		switch {
		// Create CRDs asap - both because they are slow and because we will likely create instances of them soon
		case gk == "apiextensions.k8s.io/CustomResourceDefinition":
			return -1000

			// We need to create ServiceAccounts, Roles before we bind them with a RoleBinding
		case gk == "/ServiceAccount" || gk == "rbac.authorization.k8s.io/ClusterRole":
			return 1
		case gk == "rbac.authorization.k8s.io/ClusterRoleBinding":
			return 2

			// validatingwebhookconfiguration is configured to FAIL-OPEN in the default install. For the
			// re-install case we want to apply the validatingwebhookconfiguration first to reset any
			// orphaned validatingwebhookconfiguration that is FAIL-CLOSE.
		case gk == "admissionregistration.k8s.io/ValidatingWebhookConfiguration":
			return 3

		case istioCustomResources(o.Group):
			return 4

			// Pods might need configmap or secrets - avoid backoff by creating them first
		case gk == "/ConfigMap" || gk == "/Secrets":
			return 100

			// Create the pods after we've created other things they might be waiting for
		case gk == "extensions/Deployment" || gk == "app/Deployment":
			return 1000

			// Autoscalers typically act on a deployment
		case gk == "autoscaling/HorizontalPodAutoscaler":
			return 1001

			// Create services late - after pods have been started
		case gk == "/Service":
			return 10000

		default:
			return 1000
		}
	}
}

func ObjectsNotInLists(objects K8sObjects, lists ...K8sObjects) K8sObjects {
	var ret K8sObjects

	filterMap := make(map[*K8sObject]bool)
	for _, list := range lists {
		for _, object := range list {
			filterMap[object] = true
		}
	}

	for _, o := range objects {
		if !filterMap[o] {
			ret = append(ret, o)
		}
	}
	return ret
}

// KindObjects returns the subset of objs with the given kind.
func KindObjects(objs K8sObjects, kind string) K8sObjects {
	var ret K8sObjects
	for _, o := range objs {
		if o.Kind == kind {
			ret = append(ret, o)
		}
	}
	return ret
}

// ParseK8SYAMLToIstioOperator parses a IstioOperator CustomResource YAML string and unmarshals in into
// an IstioOperatorSpec object. It returns the object and an API group/version with it.
func ParseK8SYAMLToIstioOperator(yml string) (*v1alpha1.IstioOperator, *schema.GroupVersionKind, error) {
	o, err := ParseYAMLToK8sObject([]byte(yml))
	if err != nil {
		return nil, nil, err
	}
	iop := &v1alpha1.IstioOperator{}
	if err := yaml.UnmarshalStrict([]byte(yml), iop); err != nil {
		return nil, nil, err
	}
	gvk := o.GroupVersionKind()
	v1alpha1.SetNamespace(iop.Spec, o.Namespace)
	return iop, &gvk, nil
}

// AllObjectHashes returns a map with object hashes of all the objects contained in cmm as the keys.
func AllObjectHashes(m string) map[string]bool {
	ret := make(map[string]bool)
	objs, err := ParseK8sObjectsFromYAMLManifest(m)
	if err != nil {
		log.Error(err.Error())
	}
	for _, o := range objs {
		ret[o.Hash()] = true
	}

	return ret
}

// resolvePDBConflict When user uses both minAvailable and
// maxUnavailable to configure istio instances, these two
// parameters are mutually exclusive, care must be taken
// to resolve the issue
func resolvePDBConflict(o *K8sObject) *K8sObject {
	if o.json == nil {
		return o
	}
	if o.object.Object["spec"] == nil {
		return o
	}
	spec := o.object.Object["spec"].(map[string]any)
	isDefault := func(item any) bool {
		var ii intstr.IntOrString
		switch item := item.(type) {
		case int:
			ii = intstr.FromInt(item)
		case int64:
			ii = intstr.FromInt(int(item))
		case string:
			ii = intstr.FromString(item)
		default:
			ii = intstr.FromInt(0)
		}
		intVal, err := intstr.GetScaledValueFromIntOrPercent(&ii, 100, false)
		if err != nil || intVal == 0 {
			return true
		}
		return false
	}
	if spec["maxUnavailable"] != nil && spec["minAvailable"] != nil {
		// When both maxUnavailable and minAvailable present and
		// neither has value 0, this is considered a conflict,
		// then maxUnavailale will take precedence.
		if !isDefault(spec["maxUnavailable"]) && !isDefault(spec["minAvailable"]) {
			delete(spec, "minAvailable")
			// Make sure that the json and yaml representation of the object
			// is consistent with the changed object
			o.json = nil
			o.json, _ = o.JSON()
			if o.yaml != nil {
				o.yaml = nil
				o.yaml, _ = o.YAML()
			}
		}
	}
	return o
}
