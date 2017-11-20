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
	"k8s.io/client-go/rest"

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
	kinds := runtime.KindMap(config.InventoryMap(adapters), info)
	vc.ResourceNames = make([]string, 0, len(kinds))
	for name := range kinds {
		vc.ResourceNames = append(vc.ResourceNames, pluralize(name))
	}
	vc.Validator = store.NewValidator(nil, kinds)
	validatorCmd := &cobra.Command{
		Use:   "validator",
		Short: "Runs an https server for validations. Works as an external admission webhook for k8s",
		Run: func(cmd *cobra.Command, args []string) {
			runValidator(vc, kinds, printf, fatalf)
		},
	}
	validatorCmd.PersistentFlags().StringVar(&vc.ServiceNamespace, "namespace", "istio-system", "the namespace where this webhook is deployed")
	validatorCmd.PersistentFlags().StringVar(&vc.ServiceName, "webhook-name", "istio-mixer-webhook", "the name of the webhook")
	validatorCmd.PersistentFlags().StringArrayVar(&vc.ValidateNamespaces, "target-namespaces", []string{},
		"the list of namespaces where changes should be validated. Empty means to validate everything. Used for test only.")
	validatorCmd.PersistentFlags().IntVarP(&vc.Port, "port", "p", 9099, "the port number of the webhook")
	validatorCmd.PersistentFlags().StringVar(&vc.SecretName, "secret-name", "", "The name of k8s secret where the certificates are stored")
	validatorCmd.PersistentFlags().DurationVar(&vc.RegistrationDelay, "registration-delay", 5*time.Second, "Time to delay webhook registration after starting webhook server")
	return validatorCmd
}

func createK8sClient() (*kubernetes.Clientset, error) {
	// Validator needs to run within a cluster. It should work as long as InClusterConfig is valid.
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

func runValidator(vc crd.ControllerOptions, kinds map[string]proto.Message, printf, fatalf shared.FormatFn) {
	client, err := createK8sClient()
	if err != nil {
		fatalf("Failed to create kubernetes client: %v", err)
	}
	vs, err := crd.NewController(client, vc)
	if err != nil {
		fatalf("Failed to create validator server: %v", err)
	}
	vs.Run(nil)
}
