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

package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"istio.io/api/label"
	"istio.io/istio/istioctl/pkg/util/handlers"
	analyzer_util "istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/kube"
	admit_v1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	"k8s.io/kubectl/pkg/util/podutils"
)

var labelPairs string

func checkInjectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check-inject [<type>/]<name>[.<namespace>]",
		Short: "Check the injection status or inject-ability of a given resource, explains why it is (or will be) injected or not",
		Long: `
Checks associated resources of the given resource, and running webhooks to examine whether the pod can be or will be injected or not.`,
		Example: `	# Check the injection status of a pod
	istioctl check-inject details-v1-fcff6c49c-kqnfk.test
	
	# Check the injection status of a pod under a deployment
	istioctl check-inject deployment/details-v1

	# Check the injection status of a pod under a deployment in namespace test
	istioctl check-inject deployment/details-v1 -n test

   # Check the injection status of a pod based on namespace and app labels
	istioctl check-inject -n test -l app=helloworld,version=v1
`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && !cmd.Flags().Lookup("labels").Changed || len(args) > 1 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("check-inject requires only [<resource-type>/]<resource-name>[.<namespace>], or specifiy labels flag")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := kubeClient(kubeconfig, configContext)
			if err != nil {
				return err
			}
			var podName, podNs string
			if len(args) == 1 {
				podName, podNs, err = handlers.InferPodInfoFromTypedResource(args[0],
					handlers.HandleNamespace(namespace, defaultNamespace),
					client.UtilFactory())
				if err != nil {
					return err
				}
			} else {
				rest, err := client.UtilFactory().ToRESTConfig()
				if err != nil {
					return err
				}
				coreClient, err := corev1client.NewForConfig(rest)
				if err != nil {
					return err
				}
				sortBy := func(pods []*corev1.Pod) sort.Interface { return podutils.ByLogging(pods) }
				timeout := 2 * time.Second
				pod, _, err := polymorphichelpers.GetFirstPod(coreClient, namespace, labelPairs, timeout, sortBy)
				podName = pod.Name
				podNs = pod.Namespace
			}
			whs, err := client.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return err
			}
			if len(whs.Items) == 0 {
				return fmt.Errorf("ERROR: no injector is found for sidecar injection")
			}
			checkResults, err := analyzeRunningWebhooks(client, whs.Items, podName, podNs)
			if err != nil {
				return err
			}
			return printCheckInjectorResults(cmd.OutOrStdout(), checkResults)
		},
	}
	cmd.PersistentFlags().StringVarP(&labelPairs, "labels", "l", "",
		"Check namespace and label pairs injection status, split multiple labels by commas")
	return cmd
}

func printCheckInjectorResults(writer io.Writer, was []webhookAnalysis) error {
	if len(was) == 0 {
		fmt.Fprintf(writer, "No Istio injection hooks present.\n")
		return nil
	}
	w := new(tabwriter.Writer).Init(writer, 0, 8, 1, ' ', 0)
	fmt.Fprintln(w, "WEBHOOK\tREVISION\tINJECTED\tREASON")
	injectedTotal := 0
	for _, ws := range was {
		injected := "\u2716"
		if ws.Injected == true {
			injected = "\u2714"
			injectedTotal++
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", ws.Name, ws.Revision, injected, ws.Reason)
	}
	if injectedTotal > 1 {
		fmt.Fprintf(w, "ERROR: multiple webhooks will inject, which can lead to errors")
	}
	return w.Flush()
}

type webhookAnalysis struct {
	Name     string
	Revision string
	Injected bool
	Reason   string
}

func analyzeRunningWebhooks(client kube.ExtendedClient, whs []admit_v1.MutatingWebhookConfiguration,
	podName, podNamespace string) ([]webhookAnalysis, error) {
	results := make([]webhookAnalysis, 0)
	ns, err := client.Kube().CoreV1().Namespaces().Get(context.TODO(), podNamespace, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	pod, err := client.Kube().CoreV1().Pods(podNamespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	for _, mwc := range whs {
		if !isIstioWebhook(&mwc) {
			continue
		}
		rev := extractRevision(&mwc)
		reason, injected := analyzeWebhooksMatchStatus(mwc.Webhooks, pod, ns, rev)
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
	return results, nil
}

func analyzeWebhooksMatchStatus(whs []admit_v1.MutatingWebhook, pod *corev1.Pod, ns *corev1.Namespace,
	revision string) (reason string, injected bool) {
	for _, wh := range whs {
		nsMatched, nsLabel := extractMatchedSelectorInfo(wh.NamespaceSelector, ns.GetLabels())
		podMatched, podLabel := extractMatchedSelectorInfo(wh.ObjectSelector, pod.GetLabels())
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
					v, ok := pod.GetLabels()[me.Key]
					if ok {
						return fmt.Sprintf("Pod has %s=%s label, preventing injection", me.Key, v), false
					}
				case metav1.LabelSelectorOpNotIn:
					v, ok := pod.GetLabels()[me.Key]
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
			if v, ok := ns.GetLabels()[analyzer_util.InjectionLabelName]; ok {
				if v != "enabled" {
					return fmt.Sprintf("Namespace has %s=%s label, preventing injection",
						analyzer_util.InjectionLabelName, v), false
				}
			}
		}
	}
	var noMatchingReason = func(revision string) string {
		if revision == "default" {
			return fmt.Sprintf("No matching namespace labels (istio-injection=enabled, istio.io/rev=default) " +
				"or pod labels (sidecar.istio.io/inject=true, istio.io/rev=default)")
		}
		return fmt.Sprintf("No matching namespace labels (istio.io/rev=%s) "+
			"or pod labels (istio.io/rev=%s)", revision, revision)
	}
	return noMatchingReason(revision), false
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

func extractRevision(wh *admit_v1.MutatingWebhookConfiguration) string {
	return wh.GetLabels()[label.IoIstioRev.Name]
}

func isIstioWebhook(wh *admit_v1.MutatingWebhookConfiguration) bool {
	for _, w := range wh.Webhooks {
		if strings.HasSuffix(w.Name, "istio.io") {
			return true
		}
	}
	return false
}
