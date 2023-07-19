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

package checkinject

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	admitv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"istio.io/api/label"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/util"
	"istio.io/istio/istioctl/pkg/writer/table"
	analyzer_util "istio.io/istio/pkg/config/analysis/analyzers/util"
)

var labelPairs string

func Cmd(ctx cli.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check-inject [<type>/]<name>[.<namespace>]",
		Short: "Check the injection status or inject-ability of a given resource, explains why it is (or will be) injected or not",
		Long: `
Checks associated resources of the given resource, and running webhooks to examine whether the pod can be or will be injected or not.`,
		Example: `  # Check the injection status of a pod
  istioctl experimental check-inject details-v1-fcff6c49c-kqnfk.test
	
  # Check the injection status of a pod under a deployment
  istioctl x check-inject deployment/details-v1

  # Check the injection status of a pod under a deployment in namespace test
  istioctl x check-inject deployment/details-v1 -n test

  # Check the injection status of label pairs in a specific namespace before actual injection 
  istioctl x check-inject -n test -l app=helloworld,version=v1
`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && labelPairs == "" || len(args) > 1 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("check-inject requires only [<resource-type>/]<resource-name>[.<namespace>], or specify labels flag")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			var podName, podNs string
			var podLabels, nsLabels map[string]string
			if len(args) == 1 {
				podName, podNs, err = ctx.InferPodInfoFromTypedResource(args[0], ctx.Namespace())
				if err != nil {
					return err
				}
				pod, err := kubeClient.Kube().CoreV1().Pods(podNs).Get(context.TODO(), podName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				ns, err := kubeClient.Kube().CoreV1().Namespaces().Get(context.TODO(), podNs, metav1.GetOptions{})
				if err != nil {
					return err
				}
				podLabels = pod.GetLabels()
				nsLabels = ns.GetLabels()
			} else {
				namespace := ctx.NamespaceOrDefault(ctx.Namespace())
				ns, err := kubeClient.Kube().CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
				if err != nil {
					return err
				}
				ls, err := metav1.ParseToLabelSelector(labelPairs)
				if err != nil {
					return err
				}
				podLabels = ls.MatchLabels
				nsLabels = ns.GetLabels()
			}
			whs, err := kubeClient.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return err
			}
			checkResults := analyzeRunningWebhooks(whs.Items, podLabels, nsLabels)
			return printCheckInjectorResults(cmd.OutOrStdout(), checkResults)
		},
	}
	cmd.PersistentFlags().StringVarP(&labelPairs, "labels", "l", "",
		"Check namespace and label pairs injection status, split multiple labels by commas")
	return cmd
}

func printCheckInjectorResults(writer io.Writer, was []webhookAnalysis) error {
	if len(was) == 0 {
		fmt.Fprintf(writer, "ERROR: no Istio injection hooks present.\n")
		return nil
	}
	w := table.NewStyleWriter(writer)
	w.SetAddRowFunc(func(obj interface{}) table.Row {
		wa := obj.(webhookAnalysis)
		row := table.Row{
			Cells: make([]table.Cell, 0),
		}
		row.Cells = append(row.Cells, table.NewCell(wa.Name), table.NewCell(wa.Revision))
		if wa.Injected {
			row.Cells = append(row.Cells, table.NewCell("✔", color.FgGreen))
		} else {
			row.Cells = append(row.Cells, table.NewCell("✘", color.FgRed))
		}
		row.Cells = append(row.Cells, table.NewCell(wa.Reason))
		return row
	})
	w.AddHeader("WEBHOOK", "REVISION", "INJECTED", "REASON")
	injectedTotal := 0
	for _, ws := range was {
		if ws.Injected {
			injectedTotal++
		}
		w.AddRow(ws)
	}
	w.Flush()
	if injectedTotal > 1 {
		fmt.Fprintf(writer, "ERROR: multiple webhooks will inject, which can lead to errors")
	}
	return nil
}

type webhookAnalysis struct {
	Name     string
	Revision string
	Injected bool
	Reason   string
}

func analyzeRunningWebhooks(whs []admitv1.MutatingWebhookConfiguration, podLabels, nsLabels map[string]string) []webhookAnalysis {
	results := make([]webhookAnalysis, 0)
	for _, mwc := range whs {
		if !isIstioWebhook(&mwc) {
			continue
		}
		rev := extractRevision(&mwc)
		reason, injected := analyzeWebhooksMatchStatus(mwc.Webhooks, podLabels, nsLabels)
		results = append(results, webhookAnalysis{
			Name:     mwc.Name,
			Revision: rev,
			Injected: injected,
			Reason:   reason,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results
}

func analyzeWebhooksMatchStatus(whs []admitv1.MutatingWebhook, podLabels, nsLabels map[string]string) (reason string, injected bool) {
	for _, wh := range whs {
		nsMatched, nsLabel := extractMatchedSelectorInfo(wh.NamespaceSelector, nsLabels)
		podMatched, podLabel := extractMatchedSelectorInfo(wh.ObjectSelector, podLabels)
		if nsMatched && podMatched {
			if nsLabel != "" && podLabel != "" {
				return fmt.Sprintf("Namespace label %s matches, and pod label %s matches", nsLabel, podLabel), true
			} else if nsLabel != "" {
				return fmt.Sprintf("Namespace label %s matches", nsLabel), true
			} else if podLabel != "" {
				return fmt.Sprintf("Pod label %s matches", podLabel), true
			}
		} else if nsMatched {
			for _, me := range wh.ObjectSelector.MatchExpressions {
				switch me.Operator {
				case metav1.LabelSelectorOpDoesNotExist:
					v, ok := podLabels[me.Key]
					if ok {
						return fmt.Sprintf("Pod has %s=%s label, preventing injection", me.Key, v), false
					}
				case metav1.LabelSelectorOpNotIn:
					v, ok := podLabels[me.Key]
					if !ok {
						continue
					}
					for _, nv := range me.Values {
						if nv == v {
							return fmt.Sprintf("Pod has %s=%s label, preventing injection", me.Key, v), false
						}
					}
				}
			}
		} else if podMatched {
			if v, ok := nsLabels[analyzer_util.InjectionLabelName]; ok {
				if v != "enabled" {
					return fmt.Sprintf("Namespace has %s=%s label, preventing injection",
						analyzer_util.InjectionLabelName, v), false
				}
			}
		}
	}
	noMatchingReason := func(whs []admitv1.MutatingWebhook) string {
		nsMatchedLabels := make([]string, 0)
		podMatchedLabels := make([]string, 0)
		extractMatchLabels := func(selector *metav1.LabelSelector) []string {
			if selector == nil {
				return nil
			}
			labels := make([]string, 0)
			for _, me := range selector.MatchExpressions {
				if me.Operator != metav1.LabelSelectorOpIn {
					continue
				}
				for _, v := range me.Values {
					labels = append(labels, fmt.Sprintf("%s=%s", me.Key, v))
				}
			}
			return labels
		}

		var isDeactived bool
		for _, wh := range whs {
			if reflect.DeepEqual(wh.NamespaceSelector, util.NeverMatch) && reflect.DeepEqual(wh.ObjectSelector, util.NeverMatch) {
				isDeactived = true
			}
			nsMatchedLabels = append(nsMatchedLabels, extractMatchLabels(wh.NamespaceSelector)...)
			podMatchedLabels = append(podMatchedLabels, extractMatchLabels(wh.ObjectSelector)...)
		}
		if isDeactived {
			return "The injection webhook is deactivated, and will never match labels."
		}
		return fmt.Sprintf("No matching namespace labels (%s) "+
			"or pod labels (%s)", strings.Join(nsMatchedLabels, ", "), strings.Join(podMatchedLabels, ", "))
	}
	return noMatchingReason(whs), false
}

func extractMatchedSelectorInfo(ls *metav1.LabelSelector, objLabels map[string]string) (matched bool, injLabel string) {
	if ls == nil {
		return true, ""
	}
	selector, err := metav1.LabelSelectorAsSelector(ls)
	if err != nil {
		return false, ""
	}
	matched = selector.Matches(labels.Set(objLabels))
	if !matched {
		return matched, ""
	}
	for _, me := range ls.MatchExpressions {
		switch me.Operator {
		case metav1.LabelSelectorOpIn, metav1.LabelSelectorOpNotIn:
			if v, exist := objLabels[me.Key]; exist {
				return matched, fmt.Sprintf("%s=%s", me.Key, v)
			}
		}
	}
	return matched, ""
}

func extractRevision(wh *admitv1.MutatingWebhookConfiguration) string {
	return wh.GetLabels()[label.IoIstioRev.Name]
}

func isIstioWebhook(wh *admitv1.MutatingWebhookConfiguration) bool {
	for _, w := range wh.Webhooks {
		if strings.HasSuffix(w.Name, "istio.io") {
			return true
		}
	}
	return false
}
