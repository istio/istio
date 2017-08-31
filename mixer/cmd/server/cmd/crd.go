// Copyright 2017 Istio Authors
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

package cmd

import (
	"bytes"
	"sort"
	gotemplate "text/template"

	"github.com/spf13/cobra"

	"istio.io/mixer/cmd/shared"
	"istio.io/mixer/pkg/handler"
	mixerRuntime "istio.io/mixer/pkg/runtime"
	"istio.io/mixer/pkg/template"
)

// Group is the K8s API group.
const Group = "config.istio.io"

// Version is the K8s API version.
const Version = "v1alpha2"

func crdCmd(tmplInfos map[string]template.Info, adapters []handler.InfoFn, printf, fatalf shared.FormatFn) *cobra.Command {
	adapterCmd := cobra.Command{
		Use:   "crd",
		Short: "CRDs (CustomResourceDefinition) available in Mixer",
	}

	adapterCmd.AddCommand(&cobra.Command{
		Use:   "all",
		Short: "List all CRDs",
		Run: func(cmd *cobra.Command, args []string) {
			printCrd(printf, fatalf, mixerRuntime.RulesKind, "istio.io.mixer", "core")
			printCrd(printf, fatalf, mixerRuntime.AttributeManifestKind, "istio.io.mixer", "core")
			listCrdsAdapters(printf, fatalf, adapters)
			listCrdsInstances(printf, fatalf, tmplInfos)
		},
	})

	adapterCmd.AddCommand(&cobra.Command{
		Use:   "adapter",
		Short: "List CRDs for available adapters",
		Run: func(cmd *cobra.Command, args []string) {
			listCrdsAdapters(printf, fatalf, adapters)
		},
	})

	adapterCmd.AddCommand(&cobra.Command{
		Use:   "instance",
		Short: "List CRDs for available instance kinds (mesh functions)",
		Run: func(cmd *cobra.Command, args []string) {
			listCrdsInstances(printf, fatalf, tmplInfos)
		},
	})

	return &adapterCmd
}

func listCrdsAdapters(printf, fatalf shared.FormatFn, infoFns []handler.InfoFn) {
	for _, infoFn := range infoFns {
		info := infoFn()
		shrtName := info.Name /* TODO make this info.shortName when related PR is in. */
		// TODO : Use the plural name from the adapter info
		printCrd(printf, fatalf, shrtName, info.Name, "mixer-adapter")
	}
}

func listCrdsInstances(printf, fatalf shared.FormatFn, infos map[string]template.Info) {
	tmplNames := make([]string, 0, len(infos))

	for name := range infos {
		tmplNames = append(tmplNames, name)
	}

	sort.Strings(tmplNames)

	for _, tmplName := range tmplNames {
		info := infos[tmplName]
		// TODO : Use the plural name from the template info
		printCrd(printf, fatalf, info.Name, info.Impl, "mixer-instance")
	}
}

type crdVar struct {
	ShrtName   string
	ImplName   string
	PluralName string
	Label      string
	Name       string
	Group      string
	Version    string
}

// pluralize gives a plural the way k8s like it.
// https://github.com/kubernetes/gengo/blob/master/namer/plural_namer.go
func pluralize(singular string) string {
	switch string(singular[len(singular)-1]) {
	case "s", "x":
		return singular + "es"
	case "y":
		return singular[:len(singular)-1] + "ies"
	default:
		return singular + "s"
	}
}

func newCrdVar(shrtName, implName, label string) *crdVar {
	plural := pluralize(shrtName)
	return &crdVar{
		ShrtName:   shrtName,
		ImplName:   implName,
		PluralName: plural,
		Label:      label,
		Name:       plural + "." + Group,
		Group:      Group,
		Version:    Version,
	}
}

func printCrd(printf, fatalf shared.FormatFn, shrtName, implName, label string) {
	crdTemplate := `kind: CustomResourceDefinition
apiVersion: apiextensions.k8s.io/v1beta1
metadata:
  name: {{.Name}}
  labels:
    package: {{.ImplName}}
    istio: {{.Label}}
spec:
  group: {{.Group}}
  names:
    kind: {{.ShrtName}}
    plural: {{.PluralName}}
    singular: {{.ShrtName}}
  scope: Namespaced
  version: {{.Version}}
---
`
	t := gotemplate.New("crd")
	w := &bytes.Buffer{}
	t, _ = t.Parse(crdTemplate)
	if err := t.Execute(w, newCrdVar(shrtName, implName, label)); err != nil {
		fatalf("Could not create CRD " + err.Error())
	}
	printf(w.String())
}
