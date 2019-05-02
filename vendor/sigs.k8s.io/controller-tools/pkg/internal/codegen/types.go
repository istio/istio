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

package codegen

import (
	"sort"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/gengo/types"
)

// APIs is the information of a collection of API
type APIs struct {
	// Domain is the domain portion of the group - e.g. k8s.io
	Domain string

	// Package is the name of the root API package - e.g. github.com/my-org/my-repo/pkg/apis
	Package string

	// Pkg the Package for the root API package
	Pkg *types.Package

	// Groups is the list of API groups found under the apis package
	Groups map[string]*APIGroup

	Rules []rbacv1.PolicyRule

	Informers map[v1.GroupVersionKind]bool
}

// GetRules get rules of the APIs
func (apis *APIs) GetRules() []rbacv1.PolicyRule {
	rules := []rbacv1.PolicyRule{}
	rulesIndex := map[v1.GroupResource]sets.String{}
	for _, rule := range apis.Rules {
		for _, g := range rule.APIGroups {
			for _, r := range rule.Resources {
				gr := v1.GroupResource{
					Group:    g,
					Resource: r,
				}
				if _, found := rulesIndex[gr]; !found {
					rulesIndex[gr] = sets.NewString()
				}
				rulesIndex[gr].Insert(rule.Verbs...)
			}
		}
	}
	for gr, v := range rulesIndex {
		verbs := v.List()
		sort.Strings(verbs)
		rule := rbacv1.PolicyRule{
			Resources: []string{gr.Resource},
			APIGroups: []string{gr.Group},
			Verbs:     verbs,
		}
		rules = append(rules, rule)
	}
	return rules
}

// APIGroup contains information of an API group.
type APIGroup struct {
	// Package is the name of the go package the api group is under - e.g. github.com/me/apiserver-helloworld/apis
	Package string
	// Domain is the domain portion of the group - e.g. k8s.io
	Domain string
	// Group is the short name of the group - e.g. mushroomkingdom
	Group      string
	GroupTitle string
	// Versions is the list of all versions for this group keyed by name
	Versions map[string]*APIVersion

	UnversionedResources map[string]*APIResource

	// Structs is a list of unversioned definitions that must be generated
	Structs []*Struct
	Pkg     *types.Package
	PkgPath string
}

// Struct contains information of a struct.
type Struct struct {
	// Name is the name of the type
	Name string
	// genClient
	GenClient     bool
	GenDeepCopy   bool
	NonNamespaced bool

	GenUnversioned bool
	// Fields is the list of fields appearing in the struct
	Fields []*Field
}

// Field contains information of a field.
type Field struct {
	// Name is the name of the field
	Name string
	// For versioned Kubernetes types, this is the versioned package
	VersionedPackage string
	// For versioned Kubernetes types, this is the unversioned package
	UnversionedImport string
	UnversionedType   string
}

// APIVersion contains information of an API version.
type APIVersion struct {
	// Domain is the group domain - e.g. k8s.io
	Domain string
	// Group is the group name - e.g. mushroomkingdom
	Group string
	// Version is the api version - e.g. v1beta1
	Version string
	// Resources is a list of resources appearing in the API version keyed by name
	Resources map[string]*APIResource
	// Pkg is the Package object from code-gen
	Pkg *types.Package
}

// APIResource contains information of an API resource.
type APIResource struct {
	// Domain is the group domain - e.g. k8s.io
	Domain string
	// Group is the group name - e.g. mushroomkingdom
	Group string
	// Version is the api version - e.g. v1beta1
	Version string
	// Kind is the resource name - e.g. PeachesCastle
	Kind string
	// Resource is the resource name - e.g. peachescastles
	Resource string
	// REST is the rest.Storage implementation used to handle requests
	// This field is optional. The standard REST implementation will be used
	// by default.
	REST string
	// Subresources is a map of subresources keyed by name
	Subresources map[string]*APISubresource
	// Type is the Type object from code-gen
	Type *types.Type
	// Strategy is name of the struct to use for the strategy
	Strategy string
	// Strategy is name of the struct to use for the strategy
	StatusStrategy string
	// NonNamespaced indicates that the resource kind is non namespaced
	NonNamespaced bool

	ShortName string

	JSONSchemaProps    v1beta1.JSONSchemaProps
	CRD                v1beta1.CustomResourceDefinition
	Validation         string
	ValidationComments string
	// DocAnnotation is a map of annotations by name for doc. e.g. warning, notes message
	DocAnnotation map[string]string
	// Categories is a list of categories the resource is part of.
	Categories []string
}

// APISubresource contains information of an API subresource.
type APISubresource struct {
	// Domain is the group domain - e.g. k8s.io
	Domain string
	// Group is the group name - e.g. mushroomkingdom
	Group string
	// Version is the api version - e.g. v1beta1
	Version string
	// Kind is the resource name - e.g. PeachesCastle
	Kind string
	// Resource is the resource name - e.g. peachescastles
	Resource string
	// Request is the subresource request type - e.g. ScaleCastle
	Request string
	// REST is the rest.Storage implementation used to handle requests
	REST string
	// Path is the subresource path - e.g. scale
	Path string

	// ImportPackage is the import statement that must appear for the Request
	ImportPackage string

	// RequestType is the type of the request
	RequestType *types.Type

	// RESTType is the type of the request handler
	RESTType *types.Type
}

// Controller contains information of a controller.
type Controller struct {
	Target   schema.GroupVersionKind
	Resource string
	Pkg      *types.Package
	Repo     string
}
