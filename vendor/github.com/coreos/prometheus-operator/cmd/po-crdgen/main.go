// Copyright 2018 The prometheus-operator Authors
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

package main

import (
	"flag"
	"fmt"
	"os"

	monitoring "github.com/coreos/prometheus-operator/pkg/apis/monitoring"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	k8sutil "github.com/coreos/prometheus-operator/pkg/k8sutil"

	crdutils "github.com/ant31/crd-validation/pkg"
	extensionsobj "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
)

var (
	cfg crdutils.Config
)

func initFlags(crdkind monitoringv1.CrdKind, flagset *flag.FlagSet) *flag.FlagSet {
	flagset.Var(&cfg.Labels, "labels", "Labels")
	flagset.Var(&cfg.Annotations, "annotations", "Annotations")
	flagset.BoolVar(&cfg.EnableValidation, "with-validation", true, "Add CRD validation field, default: true")
	flagset.StringVar(&cfg.Group, "apigroup", monitoring.GroupName, "CRD api group")
	flagset.StringVar(&cfg.SpecDefinitionName, "spec-name", crdkind.SpecName, "CRD spec definition name")
	flagset.StringVar(&cfg.OutputFormat, "output", "yaml", "output format: json|yaml")
	flagset.StringVar(&cfg.Kind, "kind", crdkind.Kind, "CRD Kind")
	flagset.StringVar(&cfg.ResourceScope, "scope", string(extensionsobj.NamespaceScoped), "CRD scope: 'Namespaced' | 'Cluster'.  Default: Namespaced")
	flagset.StringVar(&cfg.Version, "version", monitoringv1.Version, "CRD version, default: 'v1'")
	flagset.StringVar(&cfg.Plural, "plural", crdkind.Plural, "CRD plural name")
	return flagset
}

func init() {
	var command *flag.FlagSet
	if len(os.Args) == 1 {
		fmt.Println("usage: po-crdgen [prometheus | alertmanager | servicemonitor | prometheusrule] [<options>]")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "prometheus":
		command = initFlags(monitoringv1.DefaultCrdKinds.Prometheus, flag.NewFlagSet("prometheus", flag.ExitOnError))
	case "servicemonitor":
		command = initFlags(monitoringv1.DefaultCrdKinds.ServiceMonitor, flag.NewFlagSet("servicemonitor", flag.ExitOnError))
	case "alertmanager":
		command = initFlags(monitoringv1.DefaultCrdKinds.Alertmanager, flag.NewFlagSet("alertmanager", flag.ExitOnError))
	case "prometheusrule":
		command = initFlags(monitoringv1.DefaultCrdKinds.PrometheusRule, flag.NewFlagSet("prometheusrule", flag.ExitOnError))
	default:
		fmt.Printf("%q is not valid command.\n choices: [prometheus, alertmanager, servicemonitor, prometheusrule]", os.Args[1])
		os.Exit(2)
	}
	command.Parse(os.Args[2:])
}

func main() {
	crd := k8sutil.NewCustomResourceDefinition(
		monitoringv1.CrdKind{Plural: cfg.Plural,
			Kind:     cfg.Kind,
			SpecName: cfg.SpecDefinitionName},
		cfg.Group, cfg.Labels.LabelsMap, cfg.EnableValidation)

	err := crdutils.MarshallCrd(crd, cfg.OutputFormat)
	if err != nil {
		fmt.Println("Error: ", err)
		os.Exit(1)
	}
	os.Exit(0)
}
