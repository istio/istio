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
	"sort"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/mixer/cmd/shared"
	pkgAdapter "istio.io/mixer/pkg/adapter"
	pkgadapter "istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/template"
)

func crdCmd(tmplInfos map[string]template.Info, adapters []pkgAdapter.InfoFn, printf shared.FormatFn) *cobra.Command {
	adapterCmd := cobra.Command{
		Use:   "crd",
		Short: "CRDs (CustomResourceDefinition) available in Mixer",
	}

	adapterCmd.AddCommand(&cobra.Command{
		Use:   "adapter",
		Short: "List CRDs for available adapters",
		Run: func(cmd *cobra.Command, args []string) {
			listCrdsAdapters(printf, adapters)
		},
	})

	adapterCmd.AddCommand(&cobra.Command{
		Use:   "instance",
		Short: "List CRDs for available instance kinds (mesh functions)",
		Run: func(cmd *cobra.Command, args []string) {
			listCrdsInstances(printf, tmplInfos)
		},
	})

	return &adapterCmd
}

func listCrdsAdapters(printf shared.FormatFn, infoFns []pkgadapter.InfoFn) {
	for _, infoFn := range infoFns {
		info := infoFn()
		shrtName := info.Name /* TODO make this info.shortName when related PR is in. */
		// TODO : Use the plural name from the adapter info
		printCrd(printf, shrtName, info.Name, shrtName+"s", "mixer-adapter")
	}
}

func listCrdsInstances(printf shared.FormatFn, infos map[string]template.Info) {
	tmplNames := make([]string, 0, len(infos))

	for name := range infos {
		tmplNames = append(tmplNames, name)
	}

	sort.Strings(tmplNames)

	for _, tmplName := range tmplNames {
		info := infos[tmplName]
		// TODO : Use the plural name from the template info
		printCrd(printf, info.Name, info.Impl, info.Name+"s", "mixer-instance")
	}
}

func printCrd(printf shared.FormatFn, shrtName, implName, pluralName, label string) {
	group := "config.istio.io"
	crd := apiextensionsv1beta1.CustomResourceDefinition{
		TypeMeta: meta_v1.TypeMeta{
			Kind:       "CustomResourceDefinition",
			APIVersion: "apiextensions.k8s.io/v1beta1",
		},
		ObjectMeta: meta_v1.ObjectMeta{
			Name: pluralName + "." + group,
			Labels: map[string]string{
				"impl":  implName,
				"istio": label,
			},
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   group,
			Version: "v1alpha2",
			Scope:   apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:   pluralName,
				Singular: shrtName,
				Kind:     shrtName,
			},
		},
	}
	out, err := yaml.Marshal(crd)
	if err != nil {
		printf("%s", err)
		return
	}
	printf(string(out))
	printf("---\n")
}
