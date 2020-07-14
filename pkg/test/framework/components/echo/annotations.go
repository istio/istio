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

package echo

import (
	"fmt"
	"strconv"
	"strings"

	"istio.io/api/annotation"
)

type AnnotationType string

const (
	WorkloadAnnotation AnnotationType = "workload"
)

type Annotation struct {
	Name    string
	Type    AnnotationType
	Default AnnotationValue
}

var (
	SidecarInject                = workloadAnnotation(annotation.SidecarInject.Name, "true")
	SidecarRewriteAppHTTPProbers = workloadAnnotation(annotation.SidecarRewriteAppHTTPProbers.Name, "")
	SidecarBootstrapOverride     = workloadAnnotation(annotation.SidecarBootstrapOverride.Name, "")
	SidecarVolumeMount           = workloadAnnotation(annotation.SidecarUserVolumeMount.Name, "")
	SidecarVolume                = workloadAnnotation(annotation.SidecarUserVolume.Name, "")
)

type AnnotationValue struct {
	Value string
}

func (v *AnnotationValue) Get() string {
	return v.Value
}

func (v *AnnotationValue) AsBool() bool {
	return toBool(v.Get())
}

func (v *AnnotationValue) AsInt() int {
	return toInt(v.Get())
}

func (v *AnnotationValue) Set(arg string) *AnnotationValue {
	v.Value = arg
	return v
}

func (v *AnnotationValue) SetBool(arg bool) *AnnotationValue {
	v.Value = strconv.FormatBool(arg)
	return v
}

func (v *AnnotationValue) SetInt(arg int) *AnnotationValue {
	v.Value = strconv.Itoa(arg)
	return v
}

func NewAnnotationValue() *AnnotationValue {
	return &AnnotationValue{}
}

func workloadAnnotation(name string, value string) Annotation {
	return Annotation{
		Name: name,
		Type: WorkloadAnnotation,
		Default: AnnotationValue{
			Value: value,
		},
	}
}

type Annotations map[Annotation]*AnnotationValue

func NewAnnotations() Annotations {
	return make(Annotations)
}

func (a Annotations) Set(k Annotation, v string) Annotations {
	a[k] = &AnnotationValue{v}
	return a
}

func (a Annotations) SetBool(k Annotation, v bool) Annotations {
	a[k] = NewAnnotationValue().SetBool(v)
	return a
}

func (a Annotations) SetInt(k Annotation, v int) Annotations {
	a[k] = NewAnnotationValue().SetInt(v)
	return a
}

func (a Annotations) getOrDefault(k Annotation) *AnnotationValue {
	anno, ok := a[k]
	if !ok {
		anno = &k.Default
	}
	return anno
}

func (a Annotations) Get(k Annotation) string {
	return a.getOrDefault(k).Value
}

func (a Annotations) GetBool(k Annotation) bool {
	return a.getOrDefault(k).AsBool()
}

func (a Annotations) GetInt(k Annotation) int {
	return a.getOrDefault(k).AsInt()
}

func toBool(v string) bool {
	switch strings.ToLower(v) {
	// http://yaml.org/type/bool.html
	case "y", "yes", "true", "on":
		return true
	default:
		return false
	}
}

func toInt(v string) int {
	i, err := strconv.Atoi(v)
	if err != nil {
		panic(fmt.Sprintf("failed parsing int value: '%s'", v))
	}
	return i
}
