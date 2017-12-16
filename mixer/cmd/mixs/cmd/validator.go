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
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/mixer/cmd/shared"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config"
	"istio.io/istio/mixer/pkg/config/crd"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/runtime"
	"istio.io/istio/mixer/pkg/template"
)

func validatorCmd(info map[string]template.Info, adapters []adapter.InfoFn, printf, fatalf shared.FormatFn) *cobra.Command {
	vc := crd.ControllerOptions{}
	var kubeconfig string
	tmplRepo := template.NewRepository(info)
	kinds := runtime.KindMap(config.AdapterInfoMap(adapters, tmplRepo.SupportsTemplate), info)
	vc.ResourceNames = make([]string, 0, len(kinds))
	for name := range kinds {
		vc.ResourceNames = append(vc.ResourceNames, pluralize(name))
	}
	vc.Validator = store.NewValidator(nil, kinds)
	validatorCmd := &cobra.Command{
		Use:   "validator",
		Short: "Runs an https server for validations. Works as an external admission webhook for k8s",
		Run: func(cmd *cobra.Command, args []string) {
			runValidator(vc, kinds, kubeconfig, printf, fatalf)
		},
	}
	validatorCmd.PersistentFlags().StringVar(&vc.ExternalAdmissionWebhookName, "external-admission-webook-name", "mixer-webhook.istio.io",
		"the name of the external admission webhook registration. Needs to be a domain with at least three segments separated by dots.")
	validatorCmd.PersistentFlags().StringVar(&vc.ServiceNamespace, "namespace", "istio-system", "the namespace where this webhook is deployed")
	validatorCmd.PersistentFlags().StringVar(&vc.ServiceName, "webhook-name", "istio-mixer-webhook", "the name of the webhook")
	validatorCmd.PersistentFlags().StringArrayVar(&vc.ValidateNamespaces, "target-namespaces", []string{},
		"the list of namespaces where changes should be validated. Empty means to validate everything. Used for test only.")
	validatorCmd.PersistentFlags().IntVarP(&vc.Port, "port", "p", 9099, "the port number of the webhook")
	validatorCmd.PersistentFlags().StringVar(&vc.SecretName, "secret-name", "", "The name of k8s secret where the certificates are stored")
	validatorCmd.PersistentFlags().DurationVar(&vc.RegistrationDelay, "registration-delay", 5*time.Second,
		"Time to delay webhook registration after starting webhook server")
	validatorCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "Use a Kubernetes configuration file instead of in-cluster configuration")
	return validatorCmd
}

func createK8sClient(kubeconfig string) (*kubernetes.Clientset, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

func runValidator(vc crd.ControllerOptions, kinds map[string]proto.Message, kubeconfig string, printf, fatalf shared.FormatFn) {
	client, err := createK8sClient(kubeconfig)
	if err != nil {
		fatalf("Failed to create kubernetes client: %v", err)
	}
	vs, err := crd.NewController(client, vc)
	if err != nil {
		fatalf("Failed to create validator server: %v", err)
	}
	vs.Run(nil)
}
