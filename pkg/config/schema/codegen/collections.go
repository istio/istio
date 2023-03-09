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

package codegen

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stoewer/go-strcase"

	"istio.io/istio/pkg/config/schema/ast"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/sets"
)

const gvkTemplate = `
// GENERATED FILE -- DO NOT EDIT
//

package {{.PackageName}}

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvr"
)

var (
{{- range .Entries }}
	{{.Resource.Identifier}} = config.GroupVersionKind{Group: "{{.Resource.Group}}", Version: "{{.Resource.Version}}", Kind: "{{.Resource.Kind}}"}
{{- end }}
)

// ToGVR converts a GVK to a GVR.
func ToGVR(g config.GroupVersionKind) (schema.GroupVersionResource, bool) {
	switch g {
{{- range .Entries }}
		case {{.Resource.Identifier}}:
			return gvr.{{.Resource.Identifier}}, true
{{- end }}
	}

	return schema.GroupVersionResource{}, false
}

// MustToGVR converts a GVK to a GVR, and panics if it cannot be converted
// Warning: this is only safe for known types; do not call on arbitrary GVKs
func MustToGVR(g config.GroupVersionKind) schema.GroupVersionResource {
	r, ok := ToGVR(g)
	if !ok {
		panic("unknown kind: " + g.String())
	}
	return r
}

// FromGVR converts a GVR to a GVK.
func FromGVR(g schema.GroupVersionResource) (config.GroupVersionKind, bool) {
	switch g {
{{- range .Entries }}
		case gvr.{{.Resource.Identifier}}:
			return {{.Resource.Identifier}}, true
{{- end }}
	}

	return config.GroupVersionKind{}, false
}

// FromGVR converts a GVR to a GVK, and panics if it cannot be converted
// Warning: this is only safe for known types; do not call on arbitrary GVRs
func MustFromGVR(g schema.GroupVersionResource) config.GroupVersionKind {
	r, ok := FromGVR(g)
	if !ok {
		panic("unknown kind: " + g.String())
	}
	return r
}
`

// nolint: lll
const crdclientTemplate = `
// GENERATED FILE -- DO NOT EDIT
//

package {{.PackageName}}

import (
	"reflect"
	"fmt"
	"context"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiinformer "sigs.k8s.io/gateway-api/pkg/client/informers/externalversions"
	"k8s.io/client-go/informers"
	istioinformer "istio.io/client-go/pkg/informers/externalversions"
	kubeextinformer "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	ktypes "istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/config"
	"k8s.io/apimachinery/pkg/runtime"
	kubeext "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	gatewayapiclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	apiistioioapiextensionsv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	apiistioioapinetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	apiistioioapinetworkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	apiistioioapisecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	apiistioioapitelemetryv1alpha1 "istio.io/client-go/pkg/apis/telemetry/v1alpha1"
{{- range .Packages}}
	{{.ImportName}} "{{.PackageName}}"
{{- end}}
)

func create(c kube.Client, cfg config.Config, objMeta metav1.ObjectMeta) (metav1.Object, error) {
	switch cfg.GroupVersionKind {
{{- range .Entries }}
	{{- if and (not .Resource.Synthetic) (not .Resource.Builtin) }}
	case gvk.{{.Resource.Identifier}}:
		return c.{{.ClientGetter}}().{{ .ClientGroupPath }}().{{ .ClientTypePath }}({{if not .Resource.ClusterScoped}}cfg.Namespace{{end}}).Create(context.TODO(), &{{ .IstioAwareClientImport }}.{{ .Resource.Kind }}{
			ObjectMeta: objMeta,
			Spec:       *(cfg.Spec.(*{{ .ClientImport }}.{{.SpecType}})),
		}, metav1.CreateOptions{})
	{{- end }}
{{- end }}
	default:
		return nil, fmt.Errorf("unsupported type: %v", cfg.GroupVersionKind)
	}
}

func update(c kube.Client, cfg config.Config, objMeta metav1.ObjectMeta) (metav1.Object, error) {
	switch cfg.GroupVersionKind {
{{- range .Entries }}
	{{- if and (not .Resource.Synthetic) (not .Resource.Builtin) }}
	case gvk.{{.Resource.Identifier}}:
		return c.{{.ClientGetter}}().{{ .ClientGroupPath }}().{{ .ClientTypePath }}({{if not .Resource.ClusterScoped}}cfg.Namespace{{end}}).Update(context.TODO(), &{{ .IstioAwareClientImport }}.{{ .Resource.Kind }}{
			ObjectMeta: objMeta,
			Spec:       *(cfg.Spec.(*{{ .ClientImport }}.{{.SpecType}})),
		}, metav1.UpdateOptions{})
	{{- end }}
{{- end }}
	default:
		return nil, fmt.Errorf("unsupported type: %v", cfg.GroupVersionKind)
	}
}

func updateStatus(c kube.Client, cfg config.Config, objMeta metav1.ObjectMeta) (metav1.Object, error) {
	switch cfg.GroupVersionKind {
{{- range .Entries }}
	{{- if and (not .Resource.Synthetic) (not .Resource.Builtin) (not (eq .StatusType "")) }}
	case gvk.{{.Resource.Identifier}}:
		return c.{{.ClientGetter}}().{{ .ClientGroupPath }}().{{ .ClientTypePath }}({{if not .Resource.ClusterScoped}}cfg.Namespace{{end}}).UpdateStatus(context.TODO(), &{{ .IstioAwareClientImport }}.{{ .Resource.Kind }}{
			ObjectMeta: objMeta,
			Status:       *(cfg.Status.(*{{ .StatusImport }}.{{.StatusType}})),
		}, metav1.UpdateOptions{})
	{{- end }}
{{- end }}
	default:
		return nil, fmt.Errorf("unsupported type: %v", cfg.GroupVersionKind)
	}
}

func patch(c kube.Client, orig config.Config, origMeta metav1.ObjectMeta, mod config.Config, modMeta metav1.ObjectMeta, typ types.PatchType) (metav1.Object, error) {
	if orig.GroupVersionKind != mod.GroupVersionKind {
		return nil, fmt.Errorf("gvk mismatch: %v, modified: %v", orig.GroupVersionKind, mod.GroupVersionKind)
	}
	switch orig.GroupVersionKind {
{{- range .Entries }}
	{{- if and (not .Resource.Synthetic) (not .Resource.Builtin) }}
	case gvk.{{.Resource.Identifier}}:
		oldRes := &{{ .IstioAwareClientImport }}.{{ .Resource.Kind }}{
				ObjectMeta: origMeta,
				Spec:       *(orig.Spec.(*{{ .ClientImport }}.{{.SpecType}})),
		}
		modRes := &{{ .IstioAwareClientImport }}.{{ .Resource.Kind }}{
			ObjectMeta: modMeta,
			Spec:       *(mod.Spec.(*{{ .ClientImport }}.{{.SpecType}})),
		}
		patchBytes, err := genPatchBytes(oldRes, modRes, typ)
		if err != nil {
				return nil, err
		}
		return c.{{.ClientGetter}}().{{ .ClientGroupPath }}().{{ .ClientTypePath }}({{if not .Resource.ClusterScoped}}orig.Namespace{{end}}).
				Patch(context.TODO(), orig.Name, typ, patchBytes, metav1.PatchOptions{FieldManager: "pilot-discovery"})
	{{- end }}
{{- end }}
	default:
		return nil, fmt.Errorf("unsupported type: %v", orig.GroupVersionKind)
	}
}


func delete(c kube.Client, typ config.GroupVersionKind, name, namespace string, resourceVersion *string) error {
	var deleteOptions metav1.DeleteOptions
	if resourceVersion != nil {
		deleteOptions.Preconditions = &metav1.Preconditions{ResourceVersion: resourceVersion}
	}
	switch typ {
{{- range .Entries }}
	{{- if and (not .Resource.Synthetic) (not .Resource.Builtin) }}
	case gvk.{{.Resource.Identifier}}:
		return c.{{.ClientGetter}}().{{ .ClientGroupPath }}().{{ .ClientTypePath }}({{if not .Resource.ClusterScoped}}namespace{{end}}).Delete(context.TODO(), name, deleteOptions)
	{{- end }}
{{- end }}
	default:
		return fmt.Errorf("unsupported type: %v", typ)
	}
}


var translationMap = map[config.GroupVersionKind]func(r runtime.Object) config.Config{
{{- range .Entries }}
	{{- if and (not .Resource.Synthetic) }}
	gvk.{{.Resource.Identifier}}: func(r runtime.Object) config.Config {
		obj := r.(*{{ .IstioAwareClientImport }}.{{.Resource.Kind}})
		return config.Config{
		  Meta: config.Meta{
		  	GroupVersionKind:  gvk.{{.Resource.Identifier}},
		  	Name:              obj.Name,
		  	Namespace:         obj.Namespace,
		  	Labels:            obj.Labels,
		  	Annotations:       obj.Annotations,
		  	ResourceVersion:   obj.ResourceVersion,
		  	CreationTimestamp: obj.CreationTimestamp.Time,
		  	OwnerReferences:   obj.OwnerReferences,
		  	UID:               string(obj.UID),
		  	Generation:        obj.Generation,
		  },
			Spec:   {{ if not .Resource.Specless }}&obj.Spec{{ else }}obj{{ end }},
      {{- if not (eq .StatusType "") }}
		  Status: &obj.Status,
      {{- end }}
		}
	},
	{{- end }}
{{- end }}
}

`

const typesTemplate = `
// GENERATED FILE -- DO NOT EDIT
//

package {{.PackageName}}

import (
	"istio.io/istio/pkg/config"

{{- range .Packages}}
	{{.ImportName}} "{{.PackageName}}"
{{- end}}
)

func GetGVK[T runtime.Object]() config.GroupVersionKind {
	i := *new(T)
	t := reflect.TypeOf(i)
	switch t {
{{- range .Entries }}
	case reflect.TypeOf(&{{ .ClientImport }}.{{ .Resource.Kind }}{}):
		return gvk.{{ .Resource.Identifier }}
{{- end }}
  default:
    panic(fmt.Sprintf("Unknown type %T", i))
	}
}
`

const clientsTemplate = `
// GENERATED FILE -- DO NOT EDIT
//

package {{.PackageName}}

import (
	"reflect"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiinformer "sigs.k8s.io/gateway-api/pkg/client/informers/externalversions"
	"k8s.io/client-go/informers"
	istioinformer "istio.io/client-go/pkg/informers/externalversions"
	kubeextinformer "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	ktypes "istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/config"
	"k8s.io/apimachinery/pkg/runtime"
	kubeext "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	gatewayapiclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
{{- range .Packages}}
	{{.ImportName}} "{{.PackageName}}"
{{- end}}
)

type ClientGetter interface {
	// Ext returns the API extensions client.
	Ext() kubeext.Interface

	// Kube returns the core kube client
	Kube() kubernetes.Interface

	// Dynamic client.
	Dynamic() dynamic.Interface

	// Metadata returns the Metadata kube client.
	Metadata() metadata.Interface

	// Istio returns the Istio kube client.
	Istio() istioclient.Interface

	// GatewayAPI returns the gateway-api kube client.
	GatewayAPI() gatewayapiclient.Interface

	// KubeInformer returns an informer for core kube client
	KubeInformer() informers.SharedInformerFactory

	// IstioInformer returns an informer for the istio client
	IstioInformer() istioinformer.SharedInformerFactory

	// GatewayAPIInformer returns an informer for the gateway-api client
	GatewayAPIInformer() gatewayapiinformer.SharedInformerFactory

	// ExtInformer returns an informer for the extension client
	ExtInformer() kubeextinformer.SharedInformerFactory
}

func GetClient[T runtime.Object](c ClientGetter, namespace string) ktypes.WriteAPI[T] {
	i := *new(T)
	t := reflect.TypeOf(i)
	switch t {
{{- range .Entries }}
	{{- if not .Resource.Synthetic }}
	case reflect.TypeOf(&{{ .ClientImport }}.{{ .Resource.Kind }}{}):
		return  c.{{.ClientGetter}}().{{ .ClientGroupPath }}().{{ .ClientTypePath }}({{if not .Resource.ClusterScoped}}namespace{{end}}).(ktypes.WriteAPI[T])
	{{- end }}
{{- end }}
  default:
    panic(fmt.Sprintf("Unknown type %T", i))
	}
}

func GetInformerFiltered[T runtime.Object](c ClientGetter, opts ktypes.InformerOptions) cache.SharedIndexInformer {
	var l func(options metav1.ListOptions) (runtime.Object, error)
	var w func(options metav1.ListOptions) (watch.Interface, error)
	i := *new(T)
	t := reflect.TypeOf(i)
	switch t {
{{- range .Entries }}
	{{- if not .Resource.Synthetic }}
	case reflect.TypeOf(&{{ .ClientImport }}.{{ .Resource.Kind }}{}):
		l = func(options metav1.ListOptions) (runtime.Object, error) {
			return c.{{.ClientGetter}}().{{ .ClientGroupPath }}().{{ .ClientTypePath }}({{if not .Resource.ClusterScoped}}""{{end}}).List(context.Background(), options)
		}
		w = func(options metav1.ListOptions) (watch.Interface, error) {
			return c.{{.ClientGetter}}().{{ .ClientGroupPath }}().{{ .ClientTypePath }}({{if not .Resource.ClusterScoped}}""{{end}}).Watch(context.Background(), options)
		}
	{{- end }}
{{- end }}
  default:
    panic(fmt.Sprintf("Unknown type %T", i))
	}
	return c.KubeInformer().InformerFor(*new(T), func(k kubernetes.Interface, resync time.Duration) cache.SharedIndexInformer {
		return cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
					options.FieldSelector = opts.FieldSelector
					options.LabelSelector = opts.LabelSelector
					return l(options)
				},
				WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
					options.FieldSelector = opts.FieldSelector
					options.LabelSelector = opts.LabelSelector
					return w(options)
				},
			},
			*new(T),
			resync,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		)
	})
}

func GetInformer[T runtime.Object](c ClientGetter) cache.SharedIndexInformer {
	i := *new(T)
	t := reflect.TypeOf(i)
	switch t {
{{- range .Entries }}
	{{- if not .Resource.Synthetic }}
	case reflect.TypeOf(&{{ .ClientImport }}.{{ .Resource.Kind }}{}):
		return  c.{{.ClientGetter}}Informer().{{ .InformerGroup }}.{{ .ClientTypePath }}().Informer()
	{{- end }}
{{- end }}
  default:
    panic(fmt.Sprintf("Unknown type %T", i))
	}
}
`

const kindTemplate = `
// GENERATED FILE -- DO NOT EDIT
//

package {{.PackageName}}

import (
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

const (
{{- range $index, $element := .Entries }}
	{{- if (eq $index 0) }}
	{{.Resource.Identifier}} Kind = iota
	{{- else }}
	{{.Resource.Identifier}}
	{{- end }}
{{- end }}
)

func (k Kind) String() string {
	switch k {
{{- range .Entries }}
	case {{.Resource.Identifier}}:
		return "{{.Resource.Kind}}"
{{- end }}
	default:
		return "Unknown"
	}
}

func MustFromGVK(g config.GroupVersionKind) Kind {
	switch g {
{{- range .Entries }}
	{{- if not (eq .Resource.Identifier "Address") }}
		case gvk.{{.Resource.Identifier}}:
			return {{.Resource.Identifier}}
	{{- end }}
{{- end }}
	}

	panic("unknown kind: " + g.String())
}
`

const collectionsTemplate = `
{{- .FilePrefix}}
// GENERATED FILE -- DO NOT EDIT
//

package {{.PackageName}}

import (
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/config/validation"
  "reflect"
{{- range .Packages}}
	{{.ImportName}} "{{.PackageName}}"
{{- end}}
)

var (
{{ range .Entries }}
	{{ .Resource.Identifier }} = resource.Builder {
			Identifier: "{{ .Resource.Identifier }}",
			Group: "{{ .Resource.Group }}",
			Kind: "{{ .Resource.Kind }}",
			Plural: "{{ .Resource.Plural }}",
			Version: "{{ .Resource.Version }}",
			{{- if .Resource.VersionAliases }}
            VersionAliases: []string{
				{{- range $alias := .Resource.VersionAliases}}
			        "{{$alias}}",
		 	    {{- end}}
			},
			{{- end}}
			Proto: "{{ .Resource.Proto }}",
			{{- if ne .Resource.StatusProto "" }}StatusProto: "{{ .Resource.StatusProto }}",{{end}}
			ReflectType: reflect.TypeOf(&{{.ClientImport}}.{{.SpecType}}{}).Elem(),
			{{- if ne .StatusType "" }}StatusType: reflect.TypeOf(&{{.StatusImport}}.{{.StatusType}}{}).Elem(), {{end}}
			ProtoPackage: "{{ .Resource.ProtoPackage }}",
			{{- if ne "" .Resource.StatusProtoPackage}}StatusPackage: "{{ .Resource.StatusProtoPackage }}", {{end}}
			ClusterScoped: {{ .Resource.ClusterScoped }},
			Synthetic: {{ .Resource.Synthetic }},
			Builtin: {{ .Resource.Builtin }},
			ValidateProto: validation.{{ .Resource.Validate }},
		}.MustBuild()
{{ end }}

	// All contains all collections in the system.
	All = collection.NewSchemasBuilder().
	{{- range .Entries }}
		MustAdd({{ .Resource.Identifier }}).
	{{- end }}
		Build()

	// Kube contains only kubernetes collections.
	Kube = collection.NewSchemasBuilder().
	{{- range .Entries }}
		{{- if or (contains .Resource.Group "k8s.io") .Resource.Builtin  }}
		MustAdd({{ .Resource.Identifier }}).
		{{- end }}
	{{- end }}
		Build()

	// Pilot contains only collections used by Pilot.
	Pilot = collection.NewSchemasBuilder().
	{{- range .Entries }}
		{{- if (contains .Resource.Group "istio.io") }}
		MustAdd({{ .Resource.Identifier }}).
		{{- end}}
	{{- end }}
		Build()

	// PilotGatewayAPI contains only collections used by Pilot, including experimental Service Api.
	PilotGatewayAPI = collection.NewSchemasBuilder().
	{{- range .Entries }}
		{{- if or (contains .Resource.Group "istio.io") (contains .Resource.Group "gateway.networking.k8s.io") }}
		MustAdd({{ .Resource.Identifier }}).
		{{- end}}
	{{- end }}
		Build()
)
`

type colEntry struct {
	Resource *ast.Resource

	// ClientImport represents the import alias for the client. Example: clientnetworkingv1alpha3.
	ClientImport string
	// ClientImport represents the import alias for the status. Example: clientnetworkingv1alpha3.
	StatusImport string
	// IstioAwareClientImport represents the import alias for the API, taking into account Istio storing its API (spec)
	// separate from its client import
	// Example: apiclientnetworkingv1alpha3.
	IstioAwareClientImport string
	// ClientGroupPath represents the group in the client. Example: NetworkingV1alpha3.
	ClientGroupPath string
	// InformerGroup represents the group in the client. Example: Networking().V1alpha3().
	InformerGroup string
	// ClientGetter returns the path to get the client from a kube.Client. Example: Istio.
	ClientGetter string
	// ClientTypePath returns the kind name. Basically upper cased "plural". Example: Gateways
	ClientTypePath string
	// SpecType returns the type of the Spec field. Example: HTTPRouteSpec.
	SpecType   string
	StatusType string
}

type inputs struct {
	Entries  []colEntry
	Packages []packageImport
}

func buildInputs() (inputs, error) {
	b, err := os.ReadFile(filepath.Join(env.IstioSrc, "pkg/config/schema/metadata.yaml"))
	if err != nil {
		fmt.Printf("unable to read input file: %v", err)
		return inputs{}, err
	}

	// Parse the file.
	m, err := ast.Parse(string(b))
	if err != nil {
		fmt.Printf("failed parsing input file: %v", err)
		return inputs{}, err
	}
	entries := make([]colEntry, 0, len(m.Resources))
	for _, r := range m.Resources {
		spl := strings.Split(r.Proto, ".")
		tname := spl[len(spl)-1]
		stat := strings.Split(r.StatusProto, ".")
		statName := stat[len(stat)-1]
		e := colEntry{
			Resource:               r,
			ClientImport:           toImport(r.ProtoPackage),
			StatusImport:           toImport(r.StatusProtoPackage),
			IstioAwareClientImport: toIstioAwareImport(r.ProtoPackage),
			ClientGroupPath:        toGroup(r.ProtoPackage),
			InformerGroup:          toInformerGroup(r.ProtoPackage),
			ClientGetter:           toGetter(r.ProtoPackage),
			ClientTypePath:         toTypePath(r),
			SpecType:               tname,
		}
		if r.StatusProtoPackage != "" {
			e.StatusType = statName
		}
		entries = append(entries, e)
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.Compare(entries[i].Resource.Identifier, entries[j].Resource.Identifier) < 0
	})

	// Single instance and sort names
	names := sets.New[string]()

	for _, r := range m.Resources {
		if r.ProtoPackage != "" {
			names.Insert(r.ProtoPackage)
		}
		if r.StatusProtoPackage != "" {
			names.Insert(r.StatusProtoPackage)
		}
	}

	packages := make([]packageImport, 0, names.Len())
	for p := range names {
		packages = append(packages, packageImport{p, toImport(p)})
	}
	sort.Slice(packages, func(i, j int) bool {
		return strings.Compare(packages[i].PackageName, packages[j].PackageName) < 0
	})

	return inputs{
		Entries:  entries,
		Packages: packages,
	}, nil
}

func toTypePath(r *ast.Resource) string {
	k := r.Kind
	g := r.Plural
	res := strings.Builder{}
	for i, c := range g {
		if i >= len(k) {
			res.WriteByte(byte(c))
		} else {
			if k[i] == bytes.ToUpper([]byte{byte(c)})[0] {
				res.WriteByte(k[i])
			} else {
				res.WriteByte(byte(c))
			}
		}
	}
	return res.String()
}

func toGetter(protoPackage string) string {
	if strings.Contains(protoPackage, "istio.io") {
		return "Istio"
	} else if strings.Contains(protoPackage, "sigs.k8s.io/gateway-api") {
		return "GatewayAPI"
	} else if strings.Contains(protoPackage, "k8s.io/apiextensions-apiserver") {
		return "Ext"
	} else {
		return "Kube"
	}
}

func toGroup(protoPackage string) string {
	p := strings.Split(protoPackage, "/")
	e := len(p) - 1
	if strings.Contains(protoPackage, "sigs.k8s.io/gateway-api") {
		// Gateway has one level of nesting with custom name
		return "Gateway" + strcase.UpperCamelCase(p[e])
	}
	// rest have two levels of nesting
	return strcase.UpperCamelCase(p[e-1]) + strcase.UpperCamelCase(p[e])
}

func toInformerGroup(protoPackage string) string {
	p := strings.Split(protoPackage, "/")
	e := len(p) - 1
	if strings.Contains(protoPackage, "sigs.k8s.io/gateway-api") {
		// Gateway has one level of nesting with custom name
		return "Gateway()." + strcase.UpperCamelCase(p[e]) + "()"
	}
	// rest have two levels of nesting
	return strcase.UpperCamelCase(p[e-1]) + "()." + strcase.UpperCamelCase(p[e]) + "()"
}

type packageImport struct {
	PackageName string
	ImportName  string
}

func toImport(p string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(p, "/", ""), ".", ""), "-", "")
}

func toIstioAwareImport(p string) string {
	imp := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(p, "/", ""), ".", ""), "-", "")
	if strings.Contains(p, "istio.io") {
		return "api" + imp
	}
	return imp
}
