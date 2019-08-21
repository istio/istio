// Copyright 2019 Istio Authors
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
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"

	"istio.io/operator/pkg/util"
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
	// TODO: replace strings with k8s const.
	case "ClusterRole", "ClusterRoleBinding":
		namespace = ""
	}
	return strings.Join([]string{kind, namespace, name}, ":")
}

// HashNameKind returns a unique, insecure hash based on kind and name.
func HashNameKind(kind, name string) string {
	return strings.Join([]string{kind, name}, ":")
}

// K8sObjectsFromUnstructuredSlice returns an Objects ptr type from a slice of Unstructured.
func K8sObjectsFromUnstructuredSlice(objs []*unstructured.Unstructured) (K8sObjects, error) {
	var ret K8sObjects
	for _, o := range objs {
		ret = append(ret, NewK8sObject(o, nil, nil))
	}
	return ret, nil
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
		return nil, fmt.Errorf("error decoding object: %v", err)
	}
	return NewK8sObject(out, nil, yaml), nil
}

// UnstructuredObject exposes the raw object, primarily for testing
func (o *K8sObject) UnstructuredObject() *unstructured.Unstructured {
	return o.object
}

// GroupKind returns the GroupKind for the K8sObject
func (o *K8sObject) GroupKind() schema.GroupKind {
	return o.object.GroupVersionKind().GroupKind()
}

// GroupVersionKind returns the GroupVersionKind for the K8sObject
func (o *K8sObject) GroupVersionKind() schema.GroupVersionKind {
	return o.object.GroupVersionKind()
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
	// TODO: there has to be a better way.
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
func (o *K8sObject) YAMLDebugString() (string, error) {
	y, err := o.YAML()
	if err != nil {
		return "", err
	}
	return string(y), nil
}

// AddLabels adds labels to the K8sObject.
// This method will override the value if there is already label with the same key.
func (o *K8sObject) AddLabels(labels map[string]string) {
	merged := make(map[string]string)
	for k, v := range o.object.GetLabels() {
		merged[k] = v
	}

	for k, v := range labels {
		merged[k] = v
	}

	o.object.SetLabels(merged)
	// Invalidate cached json
	o.json = nil
	o.yaml = nil
}

// K8sObjects holds a collection of k8s objects, so that we can filter / sequence them
type K8sObjects []*K8sObject

// ParseK8sObjectsFromYAMLManifest returns a K8sObjects represetation of manifest.
func ParseK8sObjectsFromYAMLManifest(manifest string) (K8sObjects, error) {
	var b bytes.Buffer

	var yamls []string
	scanner := bufio.NewScanner(strings.NewReader(manifest))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
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
			log.Errorf("Failed to parse YAML to a k8s object: %v", err.Error())
			continue
		}

		objects = append(objects, o)
	}

	return objects, nil
}

func removeNonYAMLLines(yms string) string {
	out := ""
	for _, s := range strings.Split(yms, "\n") {
		if strings.HasPrefix(s, "#") {
			continue
		}
		out += s + "\n"
	}

	// helm charts sometimes emits blank objects with just a "disabled" comment.
	return strings.TrimSpace(out)
}

// JSONManifest returns a JSON representation of K8sObjects os.
func (os K8sObjects) JSONManifest() (string, error) {
	var b bytes.Buffer

	for i, item := range os {
		if i != 0 {
			if _, err := b.WriteString("\n\n"); err != nil {
				return "", err
			}
		}
		// We build a JSON manifest because conversion to yaml is harder
		// (and we've lost line numbers anyway if we applied any transforms)
		json, err := item.JSON()
		if err != nil {
			return "", fmt.Errorf("error building json: %v", err)
		}
		if _, err := b.Write(json); err != nil {
			return "", err
		}
	}

	return b.String(), nil
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

// Valid checks returns true if Kind and Name of K8sObject are both not empty.
func (o *K8sObject) Valid() bool {
	if o.Kind == "" || o.Name == "" {
		return false
	}
	return true
}

func ManifestDiff(a, b string) (string, error) {
	ao, err := ParseK8sObjectsFromYAMLManifest(a)
	if err != nil {
		return "", err
	}
	bo, err := ParseK8sObjectsFromYAMLManifest(b)
	if err != nil {
		return "", err
	}

	aom, bom := ao.ToMap(), bo.ToMap()
	return manifestDiff(aom, bom)
}

// ManifestDiffWithSelect checks the manifest differences with selected and ignored resources.
// The selected filter will apply before the ignored filter.
func ManifestDiffWithSelectAndIgnore(a, b, selectResources, ignoreResources string) (string, error) {
	aosm, err := filterResourceWithSelectAndIgnore(a, selectResources, ignoreResources)
	if err != nil {
		return "", err
	}
	bosm, err := filterResourceWithSelectAndIgnore(b, selectResources, ignoreResources)
	if err != nil {
		return "", err
	}

	return manifestDiff(aosm, bosm)
}

// filterResourceWithSelectAndIgnore filter the input resources with selected and ignored filter.
func filterResourceWithSelectAndIgnore(a, selectResources, ignoreResources string) (map[string]*K8sObject, error) {
	ao, err := ParseK8sObjectsFromYAMLManifest(a)
	if err != nil {
		return nil, err
	}
	aom := ao.ToMap()
	aosm := make(map[string]*K8sObject)
	selections := strings.Split(selectResources, ",")
	ignores := strings.Split(ignoreResources, ",")
	for ak, av := range aom {
		if strings.Compare(selectResources, "") != 0 {
			for _, selected := range selections {
				re, err := buildResourceRegexp(strings.TrimSpace(selected))
				if err != nil {
					return nil, fmt.Errorf("error building the resource regexp: %v", err)
				}
				if re.MatchString(ak) {
					aosm[ak] = av
				}
			}
		}
		if strings.Compare(ignoreResources, "") != 0 {
			for _, ignored := range ignores {
				re, err := buildResourceRegexp(strings.TrimSpace(ignored))
				if err != nil {
					return nil, fmt.Errorf("error building the resource regexp: %v", err)
				}
				if re.MatchString(ak) {
					if _, ok := aosm[ak]; ok {
						delete(aosm, ak)
					}
				}
			}
		}
	}
	return aosm, nil
}

// buildResourceRegexp translates the resource indicator to regexp.
func buildResourceRegexp(s string) (*regexp.Regexp, error) {
	hash := strings.Split(s, ":")
	for i, v := range hash {
		if v == "" || v == "*" {
			hash[i] = ".*"
		}
	}
	return regexp.Compile(strings.Join(hash, ":"))
}

// manifestDiff an internal function to compare the manifests difference specified in the input.
func manifestDiff(aom, bom map[string]*K8sObject) (string, error) {
	var sb strings.Builder
	for ak, av := range aom {
		ay, err := av.YAML()
		if err != nil {
			return "", err
		}
		bo := bom[ak]
		if bo == nil {
			writeStringSafe(&sb, "\n\nObject "+ak+" is missing in B:\n\n")
			continue
		}
		by, err := bo.YAML()
		if err != nil {
			return "", err
		}
		diff := util.YAMLDiff(string(ay), string(by))
		if diff != "" {
			writeStringSafe(&sb, "\n\nObject "+ak+" has diffs:\n\n")
			writeStringSafe(&sb, diff)
		}
	}
	for bk := range bom {
		ao := aom[bk]
		if ao == nil {
			writeStringSafe(&sb, "\n\nObject "+bk+" is missing in A:\n\n")
			continue
		}
	}
	return sb.String(), nil
}

func writeStringSafe(sb io.StringWriter, s string) {
	_, err := sb.WriteString(s)
	if err != nil {
		log.Error(err.Error())
	}
}
