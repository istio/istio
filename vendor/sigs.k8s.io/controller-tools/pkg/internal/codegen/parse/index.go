/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package parse

import (
	"fmt"
	"log"
	"strings"

	"github.com/markbates/inflect"
	"k8s.io/gengo/types"
	"sigs.k8s.io/controller-tools/pkg/internal/codegen"
	"sigs.k8s.io/controller-tools/pkg/internal/general"
)

// parseIndex indexes all types with the comment "// +resource=RESOURCE" by GroupVersionKind and
// GroupKindVersion
func (b *APIs) parseIndex() {
	// Index resource by group, version, kind
	b.ByGroupVersionKind = map[string]map[string]map[string]*codegen.APIResource{}

	// Index resources by group, kind, version
	b.ByGroupKindVersion = map[string]map[string]map[string]*codegen.APIResource{}

	// Index subresources by group, version, kind
	b.SubByGroupVersionKind = map[string]map[string]map[string]*types.Type{}

	for _, c := range b.context.Order {
		// The type is a subresource, add it to the subresource index
		if IsAPISubresource(c) {
			group := GetGroup(c)
			version := GetVersion(c, group)
			kind := GetKind(c, group)
			if _, f := b.SubByGroupVersionKind[group]; !f {
				b.SubByGroupVersionKind[group] = map[string]map[string]*types.Type{}
			}
			if _, f := b.SubByGroupVersionKind[group][version]; !f {
				b.SubByGroupVersionKind[group][version] = map[string]*types.Type{}
			}
			b.SubByGroupVersionKind[group][version][kind] = c
		}

		// If it isn't a subresource or resource, continue to the next type
		if !IsAPIResource(c) {
			continue
		}

		// Parse out the resource information
		r := &codegen.APIResource{
			Type:          c,
			NonNamespaced: IsNonNamespaced(c),
		}
		r.Group = GetGroup(c)
		r.Version = GetVersion(c, r.Group)
		r.Kind = GetKind(c, r.Group)
		r.Domain = b.Domain

		// TODO: revisit the part...
		if r.Resource == "" {
			r.Resource = strings.ToLower(inflect.Pluralize(r.Kind))
		}
		rt, err := parseResourceAnnotation(c)
		if err != nil {
			log.Fatalf("failed to parse resource annotations, error: %v", err.Error())
		}
		if rt.Resource != "" {
			r.Resource = rt.Resource
		}
		r.ShortName = rt.ShortName

		// Copy the Status strategy to mirror the non-status strategy
		r.StatusStrategy = strings.TrimSuffix(r.Strategy, "Strategy")
		r.StatusStrategy = fmt.Sprintf("%sStatusStrategy", r.StatusStrategy)

		// Initialize the map entries so they aren't nill
		if _, f := b.ByGroupKindVersion[r.Group]; !f {
			b.ByGroupKindVersion[r.Group] = map[string]map[string]*codegen.APIResource{}
		}
		if _, f := b.ByGroupKindVersion[r.Group][r.Kind]; !f {
			b.ByGroupKindVersion[r.Group][r.Kind] = map[string]*codegen.APIResource{}
		}
		if _, f := b.ByGroupVersionKind[r.Group]; !f {
			b.ByGroupVersionKind[r.Group] = map[string]map[string]*codegen.APIResource{}
		}
		if _, f := b.ByGroupVersionKind[r.Group][r.Version]; !f {
			b.ByGroupVersionKind[r.Group][r.Version] = map[string]*codegen.APIResource{}
		}

		// Add the resource to the map
		b.ByGroupKindVersion[r.Group][r.Kind][r.Version] = r
		b.ByGroupVersionKind[r.Group][r.Version][r.Kind] = r
		r.Type = c
	}
}

// resourceTags contains the tags present in a "+resource=" comment
type resourceTags struct {
	Resource  string
	REST      string
	Strategy  string
	ShortName string
}

// resourceAnnotationValue is a helper function to extract resource annotation.
func resourceAnnotationValue(tag string) (resourceTags, error) {
	res := resourceTags{}
	for _, elem := range strings.Split(tag, ",") {
		key, value, err := general.ParseKV(elem)
		if err != nil {
			return resourceTags{}, fmt.Errorf("// +kubebuilder:resource: tags must be key value pairs.  Expected "+
				"keys [path=<resourcepath>] "+
				"Got string: [%s]", tag)
		}
		switch key {
		case "path":
			res.Resource = value
		case "shortName":
			res.ShortName = value
		default:
			return resourceTags{}, fmt.Errorf("The given input %s is invalid", value)
		}
	}
	return res, nil
}

// parseResourceAnnotation parses the tags in a "+resource=" comment into a resourceTags struct.
func parseResourceAnnotation(t *types.Type) (resourceTags, error) {
	finalResult := resourceTags{}
	var resourceAnnotationFound bool
	for _, comment := range t.CommentLines {
		anno := general.GetAnnotation(comment, "kubebuilder:resource")
		if len(anno) == 0 {
			continue
		}
		result, err := resourceAnnotationValue(anno)
		if err != nil {
			return resourceTags{}, err
		}
		if resourceAnnotationFound {
			return resourceTags{}, fmt.Errorf("resource annotation should only exists once per type")
		}
		resourceAnnotationFound = true
		finalResult = result
	}
	return finalResult, nil
}
