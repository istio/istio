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
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/gengo/types"
)

// parseCRDs populates the CRD field of each Group.Version.Resource,
// creating validations using the annotations on type fields.
func (b *APIs) parseCRDs() {
	for _, group := range b.APIs.Groups {
		for _, version := range group.Versions {
			for _, resource := range version.Resources {
				if IsAPIResource(resource.Type) {
					resource.JSONSchemaProps, resource.Validation =
						b.typeToJSONSchemaProps(resource.Type, sets.NewString(), []string{}, true)

					// Note: Drop the Type field at the root level of validation
					// schema. Refer to following issue for details.
					// https://github.com/kubernetes/kubernetes/issues/65293
					resource.JSONSchemaProps.Type = ""
					j, err := json.MarshalIndent(resource.JSONSchemaProps, "", "    ")
					if err != nil {
						log.Fatalf("Could not Marshall validation %v\n", err)
					}
					resource.ValidationComments = string(j)

					resource.CRD = v1beta1.CustomResourceDefinition{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "apiextensions.k8s.io/v1beta1",
							Kind:       "CustomResourceDefinition",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:   fmt.Sprintf("%s.%s.%s", resource.Resource, resource.Group, resource.Domain),
							Labels: map[string]string{"controller-tools.k8s.io": "1.0"},
						},
						Spec: v1beta1.CustomResourceDefinitionSpec{
							Group:   fmt.Sprintf("%s.%s", resource.Group, resource.Domain),
							Version: resource.Version,
							Names: v1beta1.CustomResourceDefinitionNames{
								Kind:   resource.Kind,
								Plural: resource.Resource,
							},
							Validation: &v1beta1.CustomResourceValidation{
								OpenAPIV3Schema: &resource.JSONSchemaProps,
							},
						},
					}
					if resource.NonNamespaced {
						resource.CRD.Spec.Scope = "Cluster"
					} else {
						resource.CRD.Spec.Scope = "Namespaced"
					}

					if hasCategories(resource.Type) {
						categoriesTag := getCategoriesTag(resource.Type)
						categories := strings.Split(categoriesTag, ",")
						resource.CRD.Spec.Names.Categories = categories
						resource.Categories = categories
					}

					if hasStatusSubresource(resource.Type) {
						if resource.CRD.Spec.Subresources == nil {
							resource.CRD.Spec.Subresources = &v1beta1.CustomResourceSubresources{}
						}
						resource.CRD.Spec.Subresources.Status = &v1beta1.CustomResourceSubresourceStatus{}
					}

					resource.CRD.Status.Conditions = []v1beta1.CustomResourceDefinitionCondition{}
					resource.CRD.Status.StoredVersions = []string{}

					if hasScaleSubresource(resource.Type) {
						if resource.CRD.Spec.Subresources == nil {
							resource.CRD.Spec.Subresources = &v1beta1.CustomResourceSubresources{}
						}
						jsonPath, err := parseScaleParams(resource.Type)
						if err != nil {
							log.Fatalf("failed in parsing CRD, error: %v", err.Error())
						}
						resource.CRD.Spec.Subresources.Scale = &v1beta1.CustomResourceSubresourceScale{
							SpecReplicasPath:   jsonPath[specReplicasPath],
							StatusReplicasPath: jsonPath[statusReplicasPath],
						}
						labelSelctor, ok := jsonPath[labelSelectorPath]
						if ok && labelSelctor != "" {
							resource.CRD.Spec.Subresources.Scale.LabelSelectorPath = &labelSelctor
						}
					}
					if hasPrintColumn(resource.Type) {
						result, err := parsePrintColumnParams(resource.Type)
						if err != nil {
							log.Fatalf("failed to parse printcolumn annotations, error: %v", err.Error())
						}
						resource.CRD.Spec.AdditionalPrinterColumns = result
					}
					if len(resource.ShortName) > 0 {
						resource.CRD.Spec.Names.ShortNames = []string{resource.ShortName}
					}
				}
			}
		}
	}
}

func (b *APIs) getTime() string {
	return `v1beta1.JSONSchemaProps{
    Type:   "string",
    Format: "date-time",
}`
}

func (b *APIs) objSchema() string {
	return `v1beta1.JSONSchemaProps{
    Type:   "object",
}`
}

// typeToJSONSchemaProps returns a JSONSchemaProps object and its serialization
// in Go that describe the JSONSchema validations for the given type.
func (b *APIs) typeToJSONSchemaProps(t *types.Type, found sets.String, comments []string, isRoot bool) (v1beta1.JSONSchemaProps, string) {
	// Special cases
	time := types.Name{Name: "Time", Package: "k8s.io/apimachinery/pkg/apis/meta/v1"}
	meta := types.Name{Name: "ObjectMeta", Package: "k8s.io/apimachinery/pkg/apis/meta/v1"}
	unstructured := types.Name{Name: "Unstructured", Package: "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"}
	intOrString := types.Name{Name: "IntOrString", Package: "k8s.io/apimachinery/pkg/util/intstr"}
	switch t.Name {
	case time:
		return v1beta1.JSONSchemaProps{
			Type:        "string",
			Format:      "date-time",
			Description: parseDescription(comments),
		}, b.getTime()
	case meta:
		return v1beta1.JSONSchemaProps{
			Type:        "object",
			Description: parseDescription(comments),
		}, b.objSchema()
	case unstructured:
		return v1beta1.JSONSchemaProps{
			Type:        "object",
			Description: parseDescription(comments),
		}, b.objSchema()
	case intOrString:
		return v1beta1.JSONSchemaProps{
			OneOf: []v1beta1.JSONSchemaProps{
				{
					Type: "string",
				},
				{
					Type: "integer",
				},
			},
			Description: parseDescription(comments),
		}, b.objSchema()
	}

	var v v1beta1.JSONSchemaProps
	var s string
	switch t.Kind {
	case types.Builtin:
		v, s = b.parsePrimitiveValidation(t, found, comments)
	case types.Struct:
		v, s = b.parseObjectValidation(t, found, comments, isRoot)
	case types.Map:
		v, s = b.parseMapValidation(t, found, comments)
	case types.Slice:
		v, s = b.parseArrayValidation(t, found, comments)
	case types.Array:
		v, s = b.parseArrayValidation(t, found, comments)
	case types.Pointer:
		v, s = b.typeToJSONSchemaProps(t.Elem, found, comments, false)
	case types.Alias:
		v, s = b.typeToJSONSchemaProps(t.Underlying, found, comments, false)
	default:
		log.Fatalf("Unknown supported Kind %v\n", t.Kind)
	}

	return v, s
}

var jsonRegex = regexp.MustCompile("json:\"([a-zA-Z,]+)\"")

type primitiveTemplateArgs struct {
	v1beta1.JSONSchemaProps
	Value       string
	Format      string
	EnumValue   string // TODO check type of enum value to match the type of field
	Description string
}

var primitiveTemplate = template.Must(template.New("map-template").Parse(
	`v1beta1.JSONSchemaProps{
    {{ if .Pattern -}}
    Pattern: "{{ .Pattern }}",
    {{ end -}}
    {{ if .Maximum -}}
    Maximum: getFloat({{ .Maximum }}),
    {{ end -}}
    {{ if .ExclusiveMaximum -}}
    ExclusiveMaximum: {{ .ExclusiveMaximum }},
    {{ end -}}
    {{ if .Minimum -}}
    Minimum: getFloat({{ .Minimum }}),
    {{ end -}}
    {{ if .ExclusiveMinimum -}}
    ExclusiveMinimum: {{ .ExclusiveMinimum }},
    {{ end -}}
    Type: "{{ .Value }}",
    {{ if .Format -}}
    Format: "{{ .Format }}",
    {{ end -}}
    {{ if .EnumValue -}}
    Enum: {{ .EnumValue }},
    {{ end -}}
    {{ if .MaxLength -}}
    MaxLength: getInt({{ .MaxLength }}),
    {{ end -}}
    {{ if .MinLength -}}
    MinLength: getInt({{ .MinLength }}),
    {{ end -}}
}`))

// parsePrimitiveValidation returns a JSONSchemaProps object and its
// serialization in Go that describe the validations for the given primitive
// type.
func (b *APIs) parsePrimitiveValidation(t *types.Type, found sets.String, comments []string) (v1beta1.JSONSchemaProps, string) {
	props := v1beta1.JSONSchemaProps{Type: string(t.Name.Name)}

	for _, l := range comments {
		getValidation(l, &props)
	}

	buff := &bytes.Buffer{}

	var n, f, s, d string
	switch t.Name.Name {
	case "int", "int64", "uint64":
		n = "integer"
		f = "int64"
	case "int32", "uint32":
		n = "integer"
		f = "int32"
	case "float", "float32":
		n = "number"
		f = "float"
	case "float64":
		n = "number"
		f = "double"
	case "bool":
		n = "boolean"
	case "string":
		n = "string"
	default:
		n = t.Name.Name
	}
	if props.Enum != nil {
		s = parseEnumToString(props.Enum)
	}
	d = parseDescription(comments)
	if err := primitiveTemplate.Execute(buff, primitiveTemplateArgs{props, n, f, s, d}); err != nil {
		log.Fatalf("%v", err)
	}
	props.Type = n
	props.Format = f
	props.Description = d
	return props, buff.String()
}

type mapTempateArgs struct {
	Result            string
	SkipMapValidation bool
}

var mapTemplate = template.Must(template.New("map-template").Parse(
	`v1beta1.JSONSchemaProps{
    Type:                 "object",
    {{if not .SkipMapValidation}}AdditionalProperties: &v1beta1.JSONSchemaPropsOrBool{
        Allows: true,
        Schema: &{{.Result}},
    },{{end}}
}`))

// parseMapValidation returns a JSONSchemaProps object and its serialization in
// Go that describe the validations for the given map type.
func (b *APIs) parseMapValidation(t *types.Type, found sets.String, comments []string) (v1beta1.JSONSchemaProps, string) {
	additionalProps, result := b.typeToJSONSchemaProps(t.Elem, found, comments, false)
	additionalProps.Description = ""
	props := v1beta1.JSONSchemaProps{
		Type:        "object",
		Description: parseDescription(comments),
	}
	parseOption := b.arguments.CustomArgs.(*Options)
	if !parseOption.SkipMapValidation {
		props.AdditionalProperties = &v1beta1.JSONSchemaPropsOrBool{
			Allows: true,
			Schema: &additionalProps}
	}
	buff := &bytes.Buffer{}
	if err := mapTemplate.Execute(buff, mapTempateArgs{Result: result, SkipMapValidation: parseOption.SkipMapValidation}); err != nil {
		log.Fatalf("%v", err)
	}
	return props, buff.String()
}

var arrayTemplate = template.Must(template.New("array-template").Parse(
	`v1beta1.JSONSchemaProps{
    Type:                 "{{.Type}}",
    {{ if .Format -}}
    Format: "{{.Format}}",
    {{ end -}}
    {{ if .MaxItems -}}
    MaxItems: getInt({{ .MaxItems }}),
    {{ end -}}
    {{ if .MinItems -}}
    MinItems: getInt({{ .MinItems }}),
    {{ end -}}
    {{ if .UniqueItems -}}
    UniqueItems: {{ .UniqueItems }},
    {{ end -}}
    {{ if .Items -}}
    Items: &v1beta1.JSONSchemaPropsOrArray{
        Schema: &{{.ItemsSchema}},
    },
    {{ end -}}
}`))

type arrayTemplateArgs struct {
	v1beta1.JSONSchemaProps
	ItemsSchema string
}

// parseArrayValidation returns a JSONSchemaProps object and its serialization in
// Go that describe the validations for the given array type.
func (b *APIs) parseArrayValidation(t *types.Type, found sets.String, comments []string) (v1beta1.JSONSchemaProps, string) {
	items, result := b.typeToJSONSchemaProps(t.Elem, found, comments, false)
	items.Description = ""
	props := v1beta1.JSONSchemaProps{
		Type:        "array",
		Items:       &v1beta1.JSONSchemaPropsOrArray{Schema: &items},
		Description: parseDescription(comments),
	}
	// To represent byte arrays in the generated code, the property of the OpenAPI definition
	// should have string as its type and byte as its format.
	if t.Name.Name == "[]byte" {
		props.Type = "string"
		props.Format = "byte"
		props.Items = nil
		props.Description = parseDescription(comments)
	}
	for _, l := range comments {
		getValidation(l, &props)
	}
	buff := &bytes.Buffer{}
	if err := arrayTemplate.Execute(buff, arrayTemplateArgs{props, result}); err != nil {
		log.Fatalf("%v", err)
	}
	return props, buff.String()
}

type objectTemplateArgs struct {
	v1beta1.JSONSchemaProps
	Fields   map[string]string
	Required []string
	IsRoot   bool
}

var objectTemplate = template.Must(template.New("object-template").Parse(
	`v1beta1.JSONSchemaProps{
	{{ if not .IsRoot -}}
    Type:                 "object",
	{{ end -}}
    Properties: map[string]v1beta1.JSONSchemaProps{
        {{ range $k, $v := .Fields -}}
        "{{ $k }}": {{ $v }},
        {{ end -}}
    },
    {{if .Required}}Required: []string{
        {{ range $k, $v := .Required -}}
        "{{ $v }}", 
        {{ end -}}
    },{{ end -}}
}`))

// parseObjectValidation returns a JSONSchemaProps object and its serialization in
// Go that describe the validations for the given object type.
func (b *APIs) parseObjectValidation(t *types.Type, found sets.String, comments []string, isRoot bool) (v1beta1.JSONSchemaProps, string) {
	buff := &bytes.Buffer{}
	props := v1beta1.JSONSchemaProps{
		Type:        "object",
		Description: parseDescription(comments),
	}

	if strings.HasPrefix(t.Name.String(), "k8s.io/api") {
		if err := objectTemplate.Execute(buff, objectTemplateArgs{props, nil, nil, false}); err != nil {
			log.Fatalf("%v", err)
		}
	} else {
		m, result, required := b.getMembers(t, found)
		props.Properties = m
		props.Required = required

		// Only add field validation for non-inlined fields
		for _, l := range comments {
			getValidation(l, &props)
		}

		if err := objectTemplate.Execute(buff, objectTemplateArgs{props, result, required, isRoot}); err != nil {
			log.Fatalf("%v", err)
		}
	}
	return props, buff.String()
}

// getValidation parses the validation tags from the comment and sets the
// validation rules on the given JSONSchemaProps.
func getValidation(comment string, props *v1beta1.JSONSchemaProps) {
	comment = strings.TrimLeft(comment, " ")
	if !strings.HasPrefix(comment, "+kubebuilder:validation:") {
		return
	}
	c := strings.Replace(comment, "+kubebuilder:validation:", "", -1)
	parts := strings.Split(c, "=")
	if len(parts) != 2 {
		log.Fatalf("Expected +kubebuilder:validation:<key>=<value> actual: %s", comment)
		return
	}
	switch parts[0] {
	case "Maximum":
		f, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			log.Fatalf("Could not parse float from %s: %v", comment, err)
			return
		}
		props.Maximum = &f
	case "ExclusiveMaximum":
		b, err := strconv.ParseBool(parts[1])
		if err != nil {
			log.Fatalf("Could not parse bool from %s: %v", comment, err)
			return
		}
		props.ExclusiveMaximum = b
	case "Minimum":
		f, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			log.Fatalf("Could not parse float from %s: %v", comment, err)
			return
		}
		props.Minimum = &f
	case "ExclusiveMinimum":
		b, err := strconv.ParseBool(parts[1])
		if err != nil {
			log.Fatalf("Could not parse bool from %s: %v", comment, err)
			return
		}
		props.ExclusiveMinimum = b
	case "MaxLength":
		i, err := strconv.Atoi(parts[1])
		v := int64(i)
		if err != nil {
			log.Fatalf("Could not parse int from %s: %v", comment, err)
			return
		}
		props.MaxLength = &v
	case "MinLength":
		i, err := strconv.Atoi(parts[1])
		v := int64(i)
		if err != nil {
			log.Fatalf("Could not parse int from %s: %v", comment, err)
			return
		}
		props.MinLength = &v
	case "Pattern":
		props.Pattern = parts[1]
	case "MaxItems":
		if props.Type == "array" {
			i, err := strconv.Atoi(parts[1])
			v := int64(i)
			if err != nil {
				log.Fatalf("Could not parse int from %s: %v", comment, err)
				return
			}
			props.MaxItems = &v
		}
	case "MinItems":
		if props.Type == "array" {
			i, err := strconv.Atoi(parts[1])
			v := int64(i)
			if err != nil {
				log.Fatalf("Could not parse int from %s: %v", comment, err)
				return
			}
			props.MinItems = &v
		}
	case "UniqueItems":
		if props.Type == "array" {
			b, err := strconv.ParseBool(parts[1])
			if err != nil {
				log.Fatalf("Could not parse bool from %s: %v", comment, err)
				return
			}
			props.UniqueItems = b
		}
	case "MultipleOf":
		f, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			log.Fatalf("Could not parse float from %s: %v", comment, err)
			return
		}
		props.MultipleOf = &f
	case "Enum":
		if props.Type != "array" {
			value := strings.Split(parts[1], ",")
			enums := []v1beta1.JSON{}
			for _, s := range value {
				checkType(props, s, &enums)
			}
			props.Enum = enums
		}
	case "Format":
		props.Format = parts[1]
	default:
		log.Fatalf("Unsupport validation: %s", comment)
	}
}

// getMembers builds maps by field name of the JSONSchemaProps and their Go
// serializations.
func (b *APIs) getMembers(t *types.Type, found sets.String) (map[string]v1beta1.JSONSchemaProps, map[string]string, []string) {
	members := map[string]v1beta1.JSONSchemaProps{}
	result := map[string]string{}
	required := []string{}

	// Don't allow recursion until we support it through refs
	// TODO: Support recursion
	if found.Has(t.Name.String()) {
		fmt.Printf("Breaking recursion for type %s", t.Name.String())
		return members, result, required
	}
	found.Insert(t.Name.String())

	for _, member := range t.Members {
		tags := jsonRegex.FindStringSubmatch(member.Tags)
		if len(tags) == 0 {
			// Skip fields without json tags
			//fmt.Printf("Skipping member %s %s\n", member.Name, member.Type.Name.String())
			continue
		}
		ts := strings.Split(tags[1], ",")
		name := member.Name
		strat := ""
		if len(ts) > 0 && len(ts[0]) > 0 {
			name = ts[0]
		}
		if len(ts) > 1 {
			strat = ts[1]
		}

		// Inline "inline" structs
		if strat == "inline" {
			m, r, re := b.getMembers(member.Type, found)
			for n, v := range m {
				members[n] = v
			}
			for n, v := range r {
				result[n] = v
			}
			required = append(required, re...)
		} else {
			m, r := b.typeToJSONSchemaProps(member.Type, found, member.CommentLines, false)
			members[name] = m
			result[name] = r
			if !strings.HasSuffix(strat, "omitempty") {
				required = append(required, name)
			}
		}
	}

	defer found.Delete(t.Name.String())
	return members, result, required
}
