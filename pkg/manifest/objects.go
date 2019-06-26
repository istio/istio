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
package manifest

import (
	"bufio"
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"

	"istio.io/pkg/log"
)

const (
	// YAMLSeparator is a separator for multi-document YAML files.
	YAMLSeparator = "\n---\n"
)

// Object is an in-memory representation of a k8s object, used for moving between different representations
// (Unstructured, JSON, YAML) with cached rendering.
type Object struct {
	object *unstructured.Unstructured

	Group     string
	Kind      string
	Name      string
	Namespace string

	json []byte
	yaml []byte
}

// NewObject creates a new Object and returns a ptr to it.
func NewObject(u *unstructured.Unstructured, json, yaml []byte) *Object {
	o := &Object{
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
	return strings.Join([]string{kind, namespace, name}, ":")
}

// HashNameKind returns a unique, insecure hash based on kind and name.
func HashNameKind(kind, name string) string {
	return strings.Join([]string{kind, name}, ":")
}

// ObjectsFromUnstructuredSlice returns an Objects ptr type from a slice of Unstructured.
func ObjectsFromUnstructuredSlice(objs []*unstructured.Unstructured) (Objects, error) {
	var ret Objects
	for _, o := range objs {
		ret = append(ret, NewObject(o, nil, nil))
	}
	return ret, nil
}

// ParseJSONToObject parses JSON to an Object.
func ParseJSONToObject(json []byte) (*Object, error) {
	o, _, err := unstructured.UnstructuredJSONScheme.Decode(json, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("error parsing json into unstructured object: %v", err)
	}

	u, ok := o.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("parsed unexpected type %T", o)
	}

	return NewObject(u, json, nil), nil
}

// ParseYAMLToObject parses YAML to an Object.
func ParseYAMLToObject(yaml []byte) (*Object, error) {
	r := bytes.NewReader(yaml)
	decoder := k8syaml.NewYAMLOrJSONDecoder(r, 1024)

	out := &unstructured.Unstructured{}
	err := decoder.Decode(out)
	if err != nil {
		return nil, fmt.Errorf("error decoding object: %v", err)
	}
	return NewObject(out, nil, yaml), nil
}

// UnstructuredObject exposes the raw object, primarily for testing
func (o *Object) UnstructuredObject() *unstructured.Unstructured {
	return o.object
}

// GroupKind returns the GroupKind for o.
func (o *Object) GroupKind() schema.GroupKind {
	return o.object.GroupVersionKind().GroupKind()
}

// GroupVersionKind returns the GroupVersionKind for o.
func (o *Object) GroupVersionKind() schema.GroupVersionKind {
	return o.object.GroupVersionKind()
}

// Hash returns a unique hash for o.
func (o *Object) Hash() string {
	return Hash(o.Kind, o.Namespace, o.Name)
}

// HashNameKind returns a hash for o based on name and kind only.
func (o *Object) HashNameKind() string {
	return HashNameKind(o.Kind, o.Name)
}

// JSON returns a JSON representation of o, using an internal cache.
func (o *Object) JSON() ([]byte, error) {
	if o.json != nil {
		return o.json, nil
	}

	b, err := o.object.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return b, nil
}

// YAML returns a YAML representation of o, using an internal cache.
func (o *Object) YAML() ([]byte, error) {
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

// YAMLDebugString returns a YAML representation of o, or an error string if the object cannot be rendered to YAML.
func (o *Object) YAMLDebugString() string {
	y, err := o.YAML()
	if err != nil {
		return fmt.Sprint(err)
	}
	return string(y)
}

// AddLabels adds labels to o.
func (o *Object) AddLabels(labels map[string]string) {
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

// Objects holds a collection of objects, so that we can filter / sequence them
type Objects []*Object

// ParseObjectsFromYAMLManifest returns an Objects represetation of manifest.
func ParseObjectsFromYAMLManifest(manifest string) (Objects, error) {
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

	var objects Objects

	for _, yaml := range yamls {
		o, err := ParseYAMLToObject([]byte(yaml))
		if err != nil {
			log.Infof("error decoding object: %s\n%s\n", err, yaml)
			continue
		}

		if o.GroupKind().Group == "" {
			continue
		}

		objects = append(objects, o)
	}

	return objects, nil
}

// JSONManifest returns a JSON representation of Objects os.
func (os Objects) JSONManifest() (string, error) {
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

// Sort will order the items in Objects in order of score, group, kind, name.  The intent is to
// have a deterministic ordering in which Objects are applied.
func (os Objects) Sort(score func(o *Object) int) {
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

// ToMap returns a map of Object hash to Object.
func (os Objects) ToMap() map[string]*Object {
	ret := make(map[string]*Object)
	for _, oo := range os {
		ret[oo.Hash()] = oo
	}
	return ret
}

// ToNameKindMap returns a map of Object name/kind hash to Object.
func (os Objects) ToNameKindMap() map[string]*Object {
	ret := make(map[string]*Object)
	for _, oo := range os {
		ret[oo.HashNameKind()] = oo
	}
	return ret
}

// YAML returns a YAML representation of o, using an internal cache.
func (os Objects) YAML() (string, error) {
	var sb strings.Builder
	for _, o := range os {
		oy, err := o.YAML()
		if err != nil {
			return "", err
		}
		_, err = sb.Write(oy)
		if err != nil {
			return "", err
		}
		_, err = sb.WriteString(YAMLSeparator)
		if err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}
