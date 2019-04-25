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
	"path"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/gengo/types"
	"sigs.k8s.io/controller-tools/pkg/internal/codegen"
)

type genUnversionedType struct {
	Type     *types.Type
	Resource *codegen.APIResource
}

func (b *APIs) parseAPIs() {
	apis := &codegen.APIs{
		Domain:    b.Domain,
		Package:   b.APIsPkg,
		Groups:    map[string]*codegen.APIGroup{},
		Rules:     b.Rules,
		Informers: b.Informers,
	}

	for group, versionMap := range b.ByGroupVersionKind {
		apiGroup := &codegen.APIGroup{
			Group:                group,
			GroupTitle:           strings.Title(group),
			Domain:               b.Domain,
			Versions:             map[string]*codegen.APIVersion{},
			UnversionedResources: map[string]*codegen.APIResource{},
		}

		for version, kindMap := range versionMap {
			apiVersion := &codegen.APIVersion{
				Domain:    b.Domain,
				Group:     group,
				Version:   version,
				Resources: map[string]*codegen.APIResource{},
			}
			for kind, resource := range kindMap {
				apiResource := &codegen.APIResource{
					Domain:         resource.Domain,
					Version:        resource.Version,
					Group:          resource.Group,
					Resource:       resource.Resource,
					Type:           resource.Type,
					REST:           resource.REST,
					Kind:           resource.Kind,
					Subresources:   resource.Subresources,
					StatusStrategy: resource.StatusStrategy,
					Strategy:       resource.Strategy,
					NonNamespaced:  resource.NonNamespaced,
					ShortName:      resource.ShortName,
				}
				parseDoc(resource, apiResource)
				apiVersion.Resources[kind] = apiResource
				// Set the package for the api version
				apiVersion.Pkg = b.context.Universe[resource.Type.Name.Package]
				// Set the package for the api group
				apiGroup.Pkg = b.context.Universe[filepath.Dir(resource.Type.Name.Package)]
				if apiGroup.Pkg != nil {
					apiGroup.PkgPath = apiGroup.Pkg.Path
				}

				apiGroup.UnversionedResources[kind] = apiResource
			}

			apiGroup.Versions[version] = apiVersion
		}
		b.parseStructs(apiGroup)
		apis.Groups[group] = apiGroup
	}
	apis.Pkg = b.context.Universe[b.APIsPkg]
	b.APIs = apis
}

func (b *APIs) parseStructs(apigroup *codegen.APIGroup) {
	remaining := []genUnversionedType{}
	for _, version := range apigroup.Versions {
		for _, resource := range version.Resources {
			remaining = append(remaining, genUnversionedType{resource.Type, resource})
		}
	}
	for _, version := range b.SubByGroupVersionKind[apigroup.Group] {
		for _, kind := range version {
			remaining = append(remaining, genUnversionedType{kind, nil})
		}
	}

	done := sets.String{}
	for len(remaining) > 0 {
		// Pop the next element from the list
		next := remaining[0]
		remaining[0] = remaining[len(remaining)-1]
		remaining = remaining[:len(remaining)-1]

		// Already processed this type.  Skip it
		if done.Has(next.Type.Name.Name) {
			continue
		}
		done.Insert(next.Type.Name.Name)

		// Generate the struct and append to the list
		result, additionalTypes := parseType(next.Type)

		// This is a resource, so generate the client
		if b.genClient(next.Type) {
			result.GenClient = true
			result.GenDeepCopy = true
		}

		if next.Resource != nil {
			result.NonNamespaced = IsNonNamespaced(next.Type)
		}

		if b.genDeepCopy(next.Type) {
			result.GenDeepCopy = true
		}
		apigroup.Structs = append(apigroup.Structs, result)

		// Add the newly discovered subtypes
		for _, at := range additionalTypes {
			remaining = append(remaining, genUnversionedType{at, nil})
		}
	}
}

// parseType parses the type into a Struct, and returns a list of types that
// need to be parsed
func parseType(t *types.Type) (*codegen.Struct, []*types.Type) {
	remaining := []*types.Type{}

	s := &codegen.Struct{
		Name:           t.Name.Name,
		GenClient:      false,
		GenUnversioned: true, // Generate unversioned structs by default
	}

	for _, c := range t.CommentLines {
		if strings.Contains(c, "+genregister:unversioned=false") {
			// Don't generate the unversioned struct
			s.GenUnversioned = false
		}
	}

	for _, member := range t.Members {
		uType := member.Type.Name.Name
		memberName := member.Name
		uImport := ""

		// Use the element type for Pointers, Maps and Slices
		mSubType := member.Type
		hasElem := false
		for mSubType.Elem != nil {
			mSubType = mSubType.Elem
			hasElem = true
		}
		if hasElem {
			// Strip the package from the field type
			uType = strings.Replace(member.Type.String(), mSubType.Name.Package+".", "", 1)
		}

		base := filepath.Base(member.Type.String())
		samepkg := t.Name.Package == mSubType.Name.Package

		// If not in the same package, calculate the import pkg
		if !samepkg {
			parts := strings.Split(base, ".")
			if len(parts) > 1 {
				// Don't generate unversioned types for core types, just use the versioned types
				if strings.HasPrefix(mSubType.Name.Package, "k8s.io/api/") {
					// Import the package under an alias so it doesn't conflict with other groups
					// having the same version
					importAlias := path.Base(path.Dir(mSubType.Name.Package)) + path.Base(mSubType.Name.Package)
					uImport = fmt.Sprintf("%s \"%s\"", importAlias, mSubType.Name.Package)
					if hasElem {
						// Replace the full package with the alias when referring to the type
						uType = strings.Replace(member.Type.String(), mSubType.Name.Package, importAlias, 1)
					} else {
						// Replace the full package with the alias when referring to the type
						uType = fmt.Sprintf("%s.%s", importAlias, parts[1])
					}
				} else {
					switch member.Type.Name.Package {
					case "k8s.io/apimachinery/pkg/apis/meta/v1":
						// Use versioned types for meta/v1
						uImport = fmt.Sprintf("%s \"%s\"", "metav1", "k8s.io/apimachinery/pkg/apis/meta/v1")
						uType = "metav1." + parts[1]
					default:
						// Use unversioned types for everything else
						t := member.Type

						if t.Elem != nil {
							// handle Pointers, Maps, Slices

							// We need to parse the package from the Type String
							t = t.Elem
							str := member.Type.String()
							startPkg := strings.LastIndexAny(str, "*]")
							endPkg := strings.LastIndexAny(str, ".")
							pkg := str[startPkg+1 : endPkg]
							name := str[endPkg+1:]
							prefix := str[:startPkg+1]

							uImportBase := path.Base(pkg)
							uImportName := path.Base(path.Dir(pkg)) + uImportBase
							uImport = fmt.Sprintf("%s \"%s\"", uImportName, pkg)

							uType = prefix + uImportName + "." + name
						} else {
							// handle non- Pointer, Maps, Slices
							pkg := t.Name.Package
							name := t.Name.Name

							// Come up with the alias the package is imported under
							// Concatenate with directory package to reduce naming collisions
							uImportBase := path.Base(pkg)
							uImportName := path.Base(path.Dir(pkg)) + uImportBase

							// Create the import statement
							uImport = fmt.Sprintf("%s \"%s\"", uImportName, pkg)

							// Create the field type name - should be <pkgalias>.<TypeName>
							uType = uImportName + "." + name
						}
					}
				}
			}
		}

		if member.Embedded {
			memberName = ""
		}

		s.Fields = append(s.Fields, &codegen.Field{
			Name:              memberName,
			VersionedPackage:  member.Type.Name.Package,
			UnversionedImport: uImport,
			UnversionedType:   uType,
		})

		// Add this member Type for processing if it isn't a primitive and
		// is part of the same API group
		if !mSubType.IsPrimitive() && GetGroup(mSubType) == GetGroup(t) {
			remaining = append(remaining, mSubType)
		}
	}
	return s, remaining
}

func (b *APIs) genClient(c *types.Type) bool {
	comments := Comments(c.CommentLines)
	resource := comments.getTag("resource", ":") + comments.getTag("kubebuilder:resource", ":")
	return len(resource) > 0
}

func (b *APIs) genDeepCopy(c *types.Type) bool {
	comments := Comments(c.CommentLines)
	return comments.hasTag("subresource-request")
}

func parseDoc(resource, apiResource *codegen.APIResource) {
	if HasDocAnnotation(resource.Type) {
		resource.DocAnnotation = getDocAnnotation(resource.Type, "warning", "note")
		apiResource.DocAnnotation = resource.DocAnnotation
	}
}
