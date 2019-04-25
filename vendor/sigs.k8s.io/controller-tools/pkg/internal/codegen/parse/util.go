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
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/gengo/types"
)

const (
	specReplicasPath   = "specpath"
	statusReplicasPath = "statuspath"
	labelSelectorPath  = "selectorpath"
	jsonPathError      = "invalid scale path. specpath, statuspath key-value pairs are required, only selectorpath key-value is optinal. For example: // +kubebuilder:subresource:scale:specpath=.spec.replica,statuspath=.status.replica,selectorpath=.spec.Label"
	printColumnName    = "name"
	printColumnType    = "type"
	printColumnDescr   = "description"
	printColumnPath    = "JSONPath"
	printColumnFormat  = "format"
	printColumnPri     = "priority"
	printColumnError   = "invalid printcolumn path. name,type, and JSONPath are required kye-value pairs and rest of the fields are optinal. For example: // +kubebuilder:printcolumn:name=abc,type=string,JSONPath=status"
)

// Options contains the parser options
type Options struct {
	SkipMapValidation bool

	// SkipRBACValidation flag determines whether to check RBAC annotations
	// for the controller or not at parse stage.
	SkipRBACValidation bool
}

// IsAPIResource returns true if either of the two conditions become true:
// 1. t has a +resource/+kubebuilder:resource comment tag
// 2. t has TypeMeta and ObjectMeta in its member list.
func IsAPIResource(t *types.Type) bool {
	for _, c := range t.CommentLines {
		if strings.Contains(c, "+resource") || strings.Contains(c, "+kubebuilder:resource") {
			return true
		}
	}

	typeMetaFound, objMetaFound := false, false
	for _, m := range t.Members {
		if m.Name == "TypeMeta" && m.Type.String() == "k8s.io/apimachinery/pkg/apis/meta/v1.TypeMeta" {
			typeMetaFound = true
		}
		if m.Name == "ObjectMeta" && m.Type.String() == "k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta" {
			objMetaFound = true
		}
		if typeMetaFound && objMetaFound {
			return true
		}
	}
	return false
}

// IsNonNamespaced returns true if t has a +nonNamespaced comment tag
func IsNonNamespaced(t *types.Type) bool {
	if !IsAPIResource(t) {
		return false
	}

	for _, c := range t.CommentLines {
		if strings.Contains(c, "+genclient:nonNamespaced") {
			return true
		}
	}

	for _, c := range t.SecondClosestCommentLines {
		if strings.Contains(c, "+genclient:nonNamespaced") {
			return true
		}
	}

	return false
}

// IsController returns true if t has a +controller or +kubebuilder:controller tag
func IsController(t *types.Type) bool {
	for _, c := range t.CommentLines {
		if strings.Contains(c, "+controller") || strings.Contains(c, "+kubebuilder:controller") {
			return true
		}
	}
	return false
}

// IsRBAC returns true if t has a +rbac or +kubebuilder:rbac tag
func IsRBAC(t *types.Type) bool {
	for _, c := range t.CommentLines {
		if strings.Contains(c, "+rbac") || strings.Contains(c, "+kubebuilder:rbac") {
			return true
		}
	}
	return false
}

// hasPrintColumn returns true if t has a +printcolumn or +kubebuilder:printcolumn annotation.
func hasPrintColumn(t *types.Type) bool {
	for _, c := range t.CommentLines {
		if strings.Contains(c, "+printcolumn") || strings.Contains(c, "+kubebuilder:printcolumn") {
			return true
		}
	}
	return false
}

// IsInformer returns true if t has a +informers or +kubebuilder:informers tag
func IsInformer(t *types.Type) bool {
	for _, c := range t.CommentLines {
		if strings.Contains(c, "+informers") || strings.Contains(c, "+kubebuilder:informers") {
			return true
		}
	}
	return false
}

// IsAPISubresource returns true if t has a +subresource-request comment tag
func IsAPISubresource(t *types.Type) bool {
	for _, c := range t.CommentLines {
		if strings.Contains(c, "+subresource-request") {
			return true
		}
	}
	return false
}

// HasSubresource returns true if t is an APIResource with one or more Subresources
func HasSubresource(t *types.Type) bool {
	if !IsAPIResource(t) {
		return false
	}
	for _, c := range t.CommentLines {
		if strings.Contains(c, "subresource") {
			return true
		}
	}
	return false
}

// hasStatusSubresource returns true if t is an APIResource annotated with
// +kubebuilder:subresource:status
func hasStatusSubresource(t *types.Type) bool {
	if !IsAPIResource(t) {
		return false
	}
	for _, c := range t.CommentLines {
		if strings.Contains(c, "+kubebuilder:subresource:status") {
			return true
		}
	}
	return false
}

// hasScaleSubresource returns true if t is an APIResource annotated with
// +kubebuilder:subresource:scale
func hasScaleSubresource(t *types.Type) bool {
	if !IsAPIResource(t) {
		return false
	}
	for _, c := range t.CommentLines {
		if strings.Contains(c, "+kubebuilder:subresource:scale") {
			return true
		}
	}
	return false
}

// hasCategories returns true if t is an APIResource annotated with
// +kubebuilder:categories
func hasCategories(t *types.Type) bool {
	if !IsAPIResource(t) {
		return false
	}

	for _, c := range t.CommentLines {
		if strings.Contains(c, "+kubebuilder:categories") {
			return true
		}
	}
	return false
}

// HasDocAnnotation returns true if t is an APIResource with doc annotation
// +kubebuilder:doc
func HasDocAnnotation(t *types.Type) bool {
	if !IsAPIResource(t) {
		return false
	}
	for _, c := range t.CommentLines {
		if strings.Contains(c, "+kubebuilder:doc") {
			return true
		}
	}
	return false
}

// IsUnversioned returns true if t is in given group, and not in versioned path.
func IsUnversioned(t *types.Type, group string) bool {
	return IsApisDir(filepath.Base(filepath.Dir(t.Name.Package))) && GetGroup(t) == group
}

// IsVersioned returns true if t is in given group, and in versioned path.
func IsVersioned(t *types.Type, group string) bool {
	dir := filepath.Base(filepath.Dir(filepath.Dir(t.Name.Package)))
	return IsApisDir(dir) && GetGroup(t) == group
}

// GetVersion returns version of t.
func GetVersion(t *types.Type, group string) string {
	if !IsVersioned(t, group) {
		panic(errors.Errorf("Cannot get version for unversioned type %v", t.Name))
	}
	return filepath.Base(t.Name.Package)
}

// GetGroup returns group of t.
func GetGroup(t *types.Type) string {
	return filepath.Base(GetGroupPackage(t))
}

// GetGroupPackage returns group package of t.
func GetGroupPackage(t *types.Type) string {
	if IsApisDir(filepath.Base(filepath.Dir(t.Name.Package))) {
		return t.Name.Package
	}
	return filepath.Dir(t.Name.Package)
}

// GetKind returns kind of t.
func GetKind(t *types.Type, group string) string {
	if !IsVersioned(t, group) && !IsUnversioned(t, group) {
		panic(errors.Errorf("Cannot get kind for type not in group %v", t.Name))
	}
	return t.Name.Name
}

// IsApisDir returns true if a directory path is a Kubernetes api directory
func IsApisDir(dir string) bool {
	return dir == "apis" || dir == "api"
}

// Comments is a structure for using comment tags on go structs and fields
type Comments []string

// GetTags returns the value for the first comment with a prefix matching "+name="
// e.g. "+name=foo\n+name=bar" would return "foo"
func (c Comments) getTag(name, sep string) string {
	for _, c := range c {
		prefix := fmt.Sprintf("+%s%s", name, sep)
		if strings.HasPrefix(c, prefix) {
			return strings.Replace(c, prefix, "", 1)
		}
	}
	return ""
}

// hasTag returns true if the Comments has a tag with the given name
func (c Comments) hasTag(name string) bool {
	for _, c := range c {
		prefix := fmt.Sprintf("+%s", name)
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

// GetTags returns the value for all comments with a prefix and separator.  E.g. for "name" and "="
// "+name=foo\n+name=bar" would return []string{"foo", "bar"}
func (c Comments) getTags(name, sep string) []string {
	tags := []string{}
	for _, c := range c {
		prefix := fmt.Sprintf("+%s%s", name, sep)
		if strings.HasPrefix(c, prefix) {
			tags = append(tags, strings.Replace(c, prefix, "", 1))
		}
	}
	return tags
}

// getCategoriesTag returns the value of the +kubebuilder:categories tags
func getCategoriesTag(c *types.Type) string {
	comments := Comments(c.CommentLines)
	resource := comments.getTag("kubebuilder:categories", "=")
	if len(resource) == 0 {
		panic(errors.Errorf("Must specify +kubebuilder:categories comment for type %v", c.Name))
	}
	return resource
}

// getDocAnnotation parse annotations of "+kubebuilder:doc:" with tags of "warning" or "doc" for control generating doc config.
// E.g. +kubebuilder:doc:warning=foo  +kubebuilder:doc:note=bar
func getDocAnnotation(t *types.Type, tags ...string) map[string]string {
	annotation := make(map[string]string)
	for _, tag := range tags {
		for _, c := range t.CommentLines {
			prefix := fmt.Sprintf("+kubebuilder:doc:%s=", tag)
			if strings.HasPrefix(c, prefix) {
				annotation[tag] = strings.Replace(c, prefix, "", 1)
			}
		}
	}
	return annotation
}

// parseByteValue returns the literal digital number values from a byte array
func parseByteValue(b []byte) string {
	elem := strings.Join(strings.Fields(fmt.Sprintln(b)), ",")
	elem = strings.TrimPrefix(elem, "[")
	elem = strings.TrimSuffix(elem, "]")
	return elem
}

// parseDescription parse comments above each field in the type definition.
func parseDescription(res []string) string {
	var temp strings.Builder
	var desc string
	for _, comment := range res {
		if !(strings.Contains(comment, "+kubebuilder") || strings.Contains(comment, "+optional")) {
			temp.WriteString(comment)
			temp.WriteString(" ")
			desc = strings.TrimRight(temp.String(), " ")
		}
	}
	return desc
}

// parseEnumToString returns a representive validated go format string from JSONSchemaProps schema
func parseEnumToString(value []v1beta1.JSON) string {
	res := "[]v1beta1.JSON{"
	prefix := "v1beta1.JSON{[]byte{"
	for _, v := range value {
		res = res + prefix + parseByteValue(v.Raw) + "}},"
	}
	return strings.TrimSuffix(res, ",") + "}"
}

// check type of enum element value to match type of field
func checkType(props *v1beta1.JSONSchemaProps, s string, enums *[]v1beta1.JSON) {

	// TODO support more types check
	switch props.Type {
	case "int", "int64", "uint64":
		if _, err := strconv.ParseInt(s, 0, 64); err != nil {
			log.Fatalf("Invalid integer value [%v] for a field of integer type", s)
		}
		*enums = append(*enums, v1beta1.JSON{Raw: []byte(fmt.Sprintf("%v", s))})
	case "int32", "unit32":
		if _, err := strconv.ParseInt(s, 0, 32); err != nil {
			log.Fatalf("Invalid integer value [%v] for a field of integer32 type", s)
		}
		*enums = append(*enums, v1beta1.JSON{Raw: []byte(fmt.Sprintf("%v", s))})
	case "float", "float32":
		if _, err := strconv.ParseFloat(s, 32); err != nil {
			log.Fatalf("Invalid float value [%v] for a field of float32 type", s)
		}
		*enums = append(*enums, v1beta1.JSON{Raw: []byte(fmt.Sprintf("%v", s))})
	case "float64":
		if _, err := strconv.ParseFloat(s, 64); err != nil {
			log.Fatalf("Invalid float value [%v] for a field of float type", s)
		}
		*enums = append(*enums, v1beta1.JSON{Raw: []byte(fmt.Sprintf("%v", s))})
	case "string":
		*enums = append(*enums, v1beta1.JSON{Raw: []byte(`"` + s + `"`)})
	}
}

// Scale subresource requires specpath, statuspath, selectorpath key values, represents for JSONPath of
// SpecReplicasPath, StatusReplicasPath, LabelSelectorPath separately. e.g.
// +kubebuilder:subresource:scale:specpath=.spec.replica,statuspath=.status.replica,selectorpath=
func parseScaleParams(t *types.Type) (map[string]string, error) {
	jsonPath := make(map[string]string)
	for _, c := range t.CommentLines {
		if strings.Contains(c, "+kubebuilder:subresource:scale") {
			paths := strings.Replace(c, "+kubebuilder:subresource:scale:", "", -1)
			path := strings.Split(paths, ",")
			if len(path) < 2 {
				return nil, fmt.Errorf(jsonPathError)
			}
			for _, s := range path {
				fmt.Printf("\n[debug] %s", s)
			}
			for _, s := range path {
				kv := strings.Split(s, "=")
				if kv[0] == specReplicasPath || kv[0] == statusReplicasPath || kv[0] == labelSelectorPath {
					jsonPath[kv[0]] = kv[1]
				} else {
					return nil, fmt.Errorf(jsonPathError)
				}
			}
			var ok bool
			_, ok = jsonPath[specReplicasPath]
			if !ok {
				return nil, fmt.Errorf(jsonPathError)
			}
			_, ok = jsonPath[statusReplicasPath]
			if !ok {
				return nil, fmt.Errorf(jsonPathError)
			}
			return jsonPath, nil
		}
	}
	return nil, fmt.Errorf(jsonPathError)
}

// printColumnKV parses key-value string formatted as "foo=bar" and returns key and value.
func printColumnKV(s string) (key, value string, err error) {
	kv := strings.SplitN(s, "=", 2)
	if len(kv) != 2 {
		err = fmt.Errorf("invalid key value pair")
		return key, value, err
	}
	key, value = kv[0], kv[1]
	if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
		value = value[1 : len(value)-1]
	}
	return key, value, err
}

// helperPrintColumn is a helper function for the parsePrintColumnParams to compute printer columns.
func helperPrintColumn(parts string, comment string) (v1beta1.CustomResourceColumnDefinition, error) {
	config := v1beta1.CustomResourceColumnDefinition{}
	var count int
	part := strings.Split(parts, ",")
	if len(part) < 3 {
		return v1beta1.CustomResourceColumnDefinition{}, fmt.Errorf(printColumnError)
	}

	for _, s := range part {
		fmt.Printf("\n[debug] %s", s)
	}
	for _, elem := range strings.Split(parts, ",") {
		key, value, err := printColumnKV(elem)
		if err != nil {
			return v1beta1.CustomResourceColumnDefinition{},
				fmt.Errorf("//+kubebuilder:printcolumn: tags must be key value pairs.Expected "+
					"keys [name=<name>,type=<type>,description=<descr>,format=<format>] "+
					"Got string: [%s]", parts)
		}
		if key == printColumnName || key == printColumnType || key == printColumnPath {
			count++
		}
		switch key {
		case printColumnName:
			config.Name = value
		case printColumnType:
			if value == "integer" || value == "number" || value == "string" || value == "boolean" || value == "date" {
				config.Type = value
			} else {
				return v1beta1.CustomResourceColumnDefinition{}, fmt.Errorf("invalid value for %s printcolumn", printColumnType)
			}
		case printColumnFormat:
			if config.Type == "integer" && (value == "int32" || value == "int64") {
				config.Format = value
			} else if config.Type == "number" && (value == "float" || value == "double") {
				config.Format = value
			} else if config.Type == "string" && (value == "byte" || value == "date" || value == "date-time" || value == "password") {
				config.Format = value
			} else {
				return v1beta1.CustomResourceColumnDefinition{}, fmt.Errorf("invalid value for %s printcolumn", printColumnFormat)
			}
		case printColumnPath:
			config.JSONPath = value
		case printColumnPri:
			i, err := strconv.Atoi(value)
			v := int32(i)
			if err != nil {
				return v1beta1.CustomResourceColumnDefinition{}, fmt.Errorf("invalid value for %s printcolumn", printColumnPri)
			}
			config.Priority = v
		case printColumnDescr:
			config.Description = value
		default:
			return v1beta1.CustomResourceColumnDefinition{}, fmt.Errorf(printColumnError)
		}
	}
	if count != 3 {
		return v1beta1.CustomResourceColumnDefinition{}, fmt.Errorf(printColumnError)
	}
	return config, nil
}

// printcolumn requires name,type,JSONPath fields and rest of the field are optional
// +kubebuilder:printcolumn:name=<name>,type=<type>,description=<desc>,JSONPath:<.spec.Name>,priority=<int32>,format=<format>
func parsePrintColumnParams(t *types.Type) ([]v1beta1.CustomResourceColumnDefinition, error) {
	result := []v1beta1.CustomResourceColumnDefinition{}
	for _, comment := range t.CommentLines {
		if strings.Contains(comment, "+kubebuilder:printcolumn") {
			parts := strings.Replace(comment, "+kubebuilder:printcolumn:", "", -1)
			res, err := helperPrintColumn(parts, comment)
			if err != nil {
				return []v1beta1.CustomResourceColumnDefinition{}, err
			}
			result = append(result, res)
		}
	}
	return result, nil
}
