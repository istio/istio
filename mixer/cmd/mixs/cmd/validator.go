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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/mixer/cmd/shared"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config"
	"istio.io/istio/mixer/pkg/config/crd"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/il/evaluator"
	runtimeConfig "istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/runtime2/validator"
	"istio.io/istio/mixer/pkg/template"
)

type runtimeValidatorOptions struct {
	configStoreURL    string
	identityAttribute string
	adapters          map[string]*adapter.Info
	templates         map[string]*template.Info
}

func validatorCmd(info map[string]template.Info, adapters []adapter.InfoFn, printf, fatalf shared.FormatFn) *cobra.Command {
	vc := crd.ControllerOptions{}
	tmplRepo := template.NewRepository(info)
	tmplInfo := make(map[string]*template.Info, len(info))
	for k := range info {
		t := info[k]
		tmplInfo[k] = &t
	}
	ainfo := config.AdapterInfoMap(adapters, tmplRepo.SupportsTemplate)
	rvc := runtimeValidatorOptions{adapters: ainfo, templates: tmplInfo}
	var kubeconfig string
	kinds := runtimeConfig.KindMap(ainfo, tmplInfo)
	vc.ResourceNames = make([]string, 0, len(kinds))
	for name := range kinds {
		vc.ResourceNames = append(vc.ResourceNames, pluralize(name))
	}
	validatorCmd := &cobra.Command{
		Use:   "validator",
		Short: "Runs an https server for validations. Works as an external admission webhook for k8s",
		Run: func(cmd *cobra.Command, args []string) {
			runValidator(vc, rvc, kinds, kubeconfig, printf, fatalf)
		},
	}
	validatorCmd.PersistentFlags().StringVar(&vc.ExternalAdmissionWebhookName, "external-admission-webook-name", "mixer-webhook.istio.io",
		"the name of the external admission webhook registration. Needs to be a domain with at least three segments separated by dots.")
	validatorCmd.PersistentFlags().StringVar(&vc.ServiceNamespace, "namespace", "istio-system", "the namespace where this webhook is deployed")
	validatorCmd.PersistentFlags().StringVar(&vc.ServiceName, "webhook-name", "istio-mixer-webhook", "the name of the webhook")
	validatorCmd.PersistentFlags().StringArrayVar(&vc.ValidateNamespaces, "target-namespaces", []string{metav1.NamespaceAll},
		"the list of namespaces where changes should be validated. Empty means to validate everything. Used for test only.")
	validatorCmd.PersistentFlags().IntVarP(&vc.Port, "port", "p", 9099, "the port number of the webhook")
	validatorCmd.PersistentFlags().StringVar(&vc.SecretName, "secret-name", "", "The name of k8s secret where the certificates are stored")
	validatorCmd.PersistentFlags().DurationVar(&vc.RegistrationDelay, "registration-delay", 5*time.Second,
		"Time to delay webhook registration after starting webhook server")
	validatorCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "Use a Kubernetes configuration file instead of in-cluster configuration")
	validatorCmd.PersistentFlags().StringVarP(&rvc.configStoreURL, "configStoreURL", "", "",
		"URL of the config store. Use k8s://path_to_kubeconfig or fs:// for file system. If path_to_kubeconfig is empty, in-cluster kubeconfig is used.")
	validatorCmd.PersistentFlags().StringVarP(&rvc.identityAttribute, "configIdentityAttribute", "", "destination.service",
		"Attribute that is used to identify applicable scopes.")
	if err := validatorCmd.PersistentFlags().MarkHidden("configIdentityAttribute"); err != nil {
		fatalf("unable to hide: %v", err)
	}

	return validatorCmd
}

func createK8sClient(kubeconfig string) (*kubernetes.Clientset, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

func newValidator(rvc runtimeValidatorOptions, kinds map[string]proto.Message) (store.BackendValidator, error) {
	s, err := store.NewRegistry(config.StoreInventory()...).NewStore(rvc.configStoreURL)
	if err != nil {
		return nil, err
	}
	eval, err := evaluator.NewILEvaluator(evaluator.DefaultCacheSize)
	if err != nil {
		return nil, err
	}
	rv, err := validator.New(eval, rvc.identityAttribute, s, rvc.adapters, rvc.templates)
	if err != nil {
		return nil, err
	}
	return store.NewValidator(rv, kinds), nil
}

func runValidator(vc crd.ControllerOptions, rvc runtimeValidatorOptions, kinds map[string]proto.Message, kubeconfig string, printf, fatalf shared.FormatFn) {
	client, err := createK8sClient(kubeconfig)
	if err != nil {
		fatalf("Failed to create kubernetes client: %v", err)
	}
	v, err := newValidator(rvc, kinds)
	if err != nil {
		fatalf("Failed to create the validatgor: %v", err)
	}
	vc.Validator = v
	vs, err := crd.NewController(client, vc)
	if err != nil {
		fatalf("Failed to create validator server: %v", err)
	}
	vs.Run(nil)
}
