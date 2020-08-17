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
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	caCertServiceAccount          = "default"
	caCertServiceAccountNamespace = "default"
	caKeyInK8sSecret              = "ca.crt"
	certKeyInK8sSecret            = "cert-chain.pem"
)

type enableCliOptions struct {
	// Whether enable the webhook configuration of Galley
	enableValidationWebhook bool
	// Whether enable the webhook configuration of Sidecar Injector
	enableMutationWebhook bool
	// The local file path of webhook CA certificate
	caCertPath string
	// The file path of the validation webhook configuration.
	validationWebhookConfigPath string
	// The service name of the validating webhook to manage
	validatingWebhookServiceName string
	// The file path of the mutating webhook configuration.
	mutatingWebhookConfigPath string
	// The service name of the mutating webhook to manage
	mutatingWebhookServiceName string
	// Max time for checking the validating webhook.
	// If the validating webhook is not ready in the given time, exit.
	// Otherwise, apply the webhook configuration.
	maxTimeForCheckingWebhookServer time.Duration
	// Max time for waiting the webhook certificate to be readable.
	maxTimeForReadingWebhookCert time.Duration
	// The name of the secret of a webhook.
	// istioctl will check whether the certificate in the secret is issued by
	// the certificate in the Kubernetes service account or the given CA cert, if any.
	webhookSecretName string
	// The namespace of the webhook secret.
	webhookSecretNameSpace string
}

// Validate return nil if no error. Otherwise, return the error.
func (opts *enableCliOptions) Validate() error {
	if !opts.enableValidationWebhook && !opts.enableMutationWebhook {
		return fmt.Errorf("no webhook to enable")
	}
	if len(opts.webhookSecretNameSpace) == 0 {
		return fmt.Errorf("--namespace <namespace-of-webhook-secret> is required")
	}
	if opts.enableValidationWebhook {
		if len(opts.validationWebhookConfigPath) == 0 {
			return fmt.Errorf("--validation-path <yaml-file> is required for the validation webhook configuration")
		}
		if len(opts.validatingWebhookServiceName) == 0 {
			return fmt.Errorf("--validation-service <service-name> is required")
		}
		if len(opts.webhookSecretName) == 0 {
			return fmt.Errorf("--webhook-secret <Kubernetes-secret-name> is required")
		}
	}
	if opts.enableMutationWebhook {
		if len(opts.mutatingWebhookConfigPath) == 0 {
			return fmt.Errorf("--injection-path <yaml-file> is required for the injection webhook configuration")
		}
		if len(opts.mutatingWebhookServiceName) == 0 {
			return fmt.Errorf("--injection-service <service-name> is required")
		}
		if len(opts.webhookSecretName) == 0 {
			return fmt.Errorf("-webhook-secret <Kubernetes-secret-name> is required")
		}
	}
	return nil
}

type disableCliOptions struct {
	// The name of the ValidatingWebhookConfiguration to manage
	validatingWebhookConfigName string
	// The name of the MutatingWebhookConfiguration to manage
	mutatingWebhookConfigName string
	// Whether disable the webhook configuration of Galley
	disableValidationWebhook bool
	// Whether disable the webhook configuration of Sidecar Injector
	disableInjectionWebhook bool
}

// Validate return nil if no error. Otherwise, return the error.
func (opts *disableCliOptions) Validate() error {
	if !opts.disableValidationWebhook && !opts.disableInjectionWebhook {
		return fmt.Errorf("no webhook configuration to disable")
	}
	if opts.disableValidationWebhook && opts.validatingWebhookConfigName == "" {
		return fmt.Errorf("validating webhook config to disable has empty name")
	}
	if opts.disableInjectionWebhook && opts.mutatingWebhookConfigName == "" {
		return fmt.Errorf("mutating webhook config to disable has empty name")
	}
	return nil
}

type statusCliOptions struct {
	// Whether display the webhook configuration of Galley
	validationWebhook bool
	// Whether display the webhook configuration of Sidecar Injector
	injectionWebhook bool
	// The name of the ValidatingWebhookConfiguration to manage
	validatingWebhookConfigName string
	// The name of the MutatingWebhookConfiguration to manage
	mutatingWebhookConfigName string
}

// Validate return nil if no error. Otherwise, return the error.
func (opts *statusCliOptions) Validate() error {
	if !opts.validationWebhook && !opts.injectionWebhook {
		return fmt.Errorf("no webhook configuration to display")
	}
	if opts.validationWebhook && opts.validatingWebhookConfigName == "" {
		return fmt.Errorf("validating webhook config to display has empty name")
	}
	if opts.injectionWebhook && opts.mutatingWebhookConfigName == "" {
		return fmt.Errorf("mutating webhook config to display has empty name")
	}
	return nil
}

// Webhook command to manage webhook configurations
func Webhook() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "webhook",
		Short: "webhook command to manage webhook configurations",
	}

	cmd.AddCommand(newEnableCmd())
	cmd.AddCommand(newDisableCmd())
	cmd.AddCommand(newStatusCmd())

	return cmd
}

func newEnableCmd() *cobra.Command {
	opts := &enableCliOptions{}
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable webhook configurations",
		Long: "This command is used to enable webhook configurations after installing Istio.\n" +
			"For previous Istio versions (e.g., 1.2, 1.3, etc), this command is not needed\n" +
			"because in previous versions webhooks manage their own configurations.",
		Example: `
# Enable the webhook configuration of Galley with the given webhook configuration
istioctl experimental post-install webhook enable --validation --webhook-secret istio.webhook.galley 
    --namespace istio-system --validation-path validatingwebhookconfiguration.yaml

# Enable the webhook configuration of Galley with the given webhook configuration and CA certificate
istioctl experimental post-install webhook enable --validation --webhook-secret istio.webhook.galley 
    --namespace istio-system --validation-path validatingwebhookconfiguration.yaml --ca-bundle-file ./k8s-ca-cert.pem
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// --namespace is used as the namespace of the webhook secret
			opts.webhookSecretNameSpace = namespace
			if err := opts.Validate(); err != nil {
				return err
			}
			client, err := createInterface(kubeconfig)
			if err != nil {
				return fmt.Errorf("err when creating Kubernetes client interface: %v", err)
			}
			err = enableWebhookConfig(client, opts, cmd.OutOrStdout())
			if err != nil {
				return fmt.Errorf("err when enabling webhook configurations: %v", err)
			}
			cmd.Println("webhook configurations have been enabled")
			return nil
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&opts.enableValidationWebhook, "validation", true, "Enable "+
		"validatation webhook (default true).")
	flags.BoolVar(&opts.enableMutationWebhook, "injection", true, "Enable "+
		"injection webhook (default true).")
	// Specifies the local file path to webhook CA bundle.
	// If empty, will read CA bundle from default Kubernetes service account.
	flags.StringVar(&opts.caCertPath, "ca-bundle-file", "",
		"PEM encoded CA bundle which will be used to validate the webhook's server certificates. "+
			"If this is empty, the kube-apisever's root CA is used if it can be confirmed to have signed "+
			"the webhook's certificates. This condition is sometimes true but is not guaranteed "+
			"(see https://kubernetes.io/docs/reference/command-line-tools-reference/kubelet-tls-bootstrapping)")
	flags.StringVar(&opts.validationWebhookConfigPath, "validation-path", "",
		"The file path of the validation webhook configuration.")
	flags.StringVar(&opts.mutatingWebhookConfigPath, "injection-path", "",
		"The file path of the injection webhook configuration.")
	flags.StringVar(&opts.validatingWebhookServiceName, "validation-service", "istio-galley",
		"The service name of the validation webhook to manage.")
	flags.StringVar(&opts.mutatingWebhookServiceName, "injection-service", "istio-pilot",
		"The service name of the injection webhook to manage.")
	flags.StringVar(&opts.webhookSecretName, "webhook-secret", "",
		"The name of an existing Kubernetes secret of a webhook. istioctl will verify that the "+
			"webhook certificate is issued by the CA certificate.")
	flags.DurationVar(&opts.maxTimeForCheckingWebhookServer, "timeout", 60*time.Second,
		"	Max time for checking the validating webhook server. If the validating webhook server is not ready"+
			"in the given time, exit. Otherwise, apply the webhook configuration.")
	flags.DurationVar(&opts.maxTimeForReadingWebhookCert, "read-cert-timeout", 60*time.Second,
		"	Max time for waiting the webhook certificate to be readable.")

	return cmd
}

func newDisableCmd() *cobra.Command {
	opts := &disableCliOptions{}
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable webhook configurations",
		Example: `
# Disable all webhooks
istioctl experimental post-install webhook disable

# Disable all webhooks except injection
istioctl experimental post-install webhook disable --injection=false
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			client, err := createInterface(kubeconfig)
			if err != nil {
				return fmt.Errorf("err when creating Kubernetes client interface: %v", err)
			}

			var response string
			cmd.Println("Are you sure to delete webhook configuration(s)?")
			_, err = fmt.Scanln(&response)
			if err != nil {
				return err
			}
			response = strings.ToUpper(response)
			if !(response == "Y" || response == "YES") {
				return nil
			}

			validationErr, injectionErr := disableWebhookConfig(client, opts)
			if validationErr != nil && injectionErr != nil {
				return fmt.Errorf("error when disabling webhook configurations. validation err: %v. injection err: %v",
					validationErr, injectionErr)
			} else if validationErr != nil {
				return fmt.Errorf("error when disabling validation webhook configuration: %v", validationErr)
			} else if injectionErr != nil {
				return fmt.Errorf("error when disabling injection webhook configuration: %v", injectionErr)
			}
			if len(opts.mutatingWebhookConfigName) > 0 && opts.disableInjectionWebhook {
				cmd.Printf("webhook configuration %v has been disabled\n", opts.mutatingWebhookConfigName)
			}
			if len(opts.validatingWebhookConfigName) > 0 && opts.disableValidationWebhook {
				cmd.Printf("webhook configuration %v has been disabled\n", opts.validatingWebhookConfigName)
			}
			return nil
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&opts.disableValidationWebhook, "validation", true, "Disable "+
		"validating webhook (default true).")
	flags.StringVar(&opts.validatingWebhookConfigName, "validation-config", "istio-galley",
		"The validating webhook configuration to disable.")
	flags.BoolVar(&opts.disableInjectionWebhook, "injection", true, "Disable "+
		"mutating webhook (default true).")
	flags.StringVar(&opts.mutatingWebhookConfigName, "injection-config", "istio-sidecar-injector",
		"The mutating webhook configuration to disable.")

	return cmd
}

// TODO (lei-tang): support "-o yaml" option and a summary option instead of yaml.
func newStatusCmd() *cobra.Command {
	opts := &statusCliOptions{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Get webhook configurations",
		Example: `
# Display the webhook configuration of Galley
istioctl experimental post-install webhook status --validation --validation-config istio-galley
# Display the webhook configuration of Galley and Sidecar Injector
istioctl experimental post-install webhook status --validation --validation-config istio-galley 
  --injection --injection-config istio-sidecar-injector
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			client, err := createInterface(kubeconfig)
			if err != nil {
				return fmt.Errorf("err when creating Kubernetes client interface: %v", err)
			}
			validationErr, injectionErr := displayWebhookConfig(client, opts, cmd.OutOrStdout())
			// Not found is not treated as an error
			if errors.IsNotFound(validationErr) && errors.IsNotFound(injectionErr) {
				cmd.Printf("validation webhook (%v) and injection webhook (%v) are not found\n",
					opts.validatingWebhookConfigName, opts.mutatingWebhookConfigName)
				return nil
			} else if errors.IsNotFound(validationErr) && injectionErr == nil {
				cmd.Printf("validation webhook (%v) is not found\n", opts.validatingWebhookConfigName)
				return nil
			} else if errors.IsNotFound(injectionErr) && validationErr == nil {
				cmd.Printf("injection webhook (%v) is not found\n", opts.mutatingWebhookConfigName)
				return nil
			}

			if validationErr != nil && injectionErr != nil {
				return fmt.Errorf("error when displaying webhook configurations. validation err: %v. injection err: %v",
					validationErr, injectionErr)
			} else if validationErr != nil {
				return fmt.Errorf("error when displaying validation webhook configuration: %v", validationErr)
			} else if injectionErr != nil {
				return fmt.Errorf("error when displaying injection webhook configuration: %v", injectionErr)
			}
			return nil
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&opts.validationWebhook, "validation", true, "Display "+
		"the validating webhook configuration.")
	flags.BoolVar(&opts.injectionWebhook, "injection", true, "Display "+
		"the injection webhook configuration.")
	flags.StringVar(&opts.validatingWebhookConfigName, "validation-config", "istio-galley",
		"The name of the ValidatingWebhookConfiguration to display.")
	flags.StringVar(&opts.mutatingWebhookConfigName, "injection-config", "istio-sidecar-injector",
		"The name of the MutatingWebhookConfiguration to display.")

	return cmd
}

// Create the validatingwebhookconfiguration
func createValidatingWebhookConfig(k8sClient kubernetes.Interface,
	config *v1beta1.ValidatingWebhookConfiguration, writer io.Writer) (*v1beta1.ValidatingWebhookConfiguration, error) {
	var whConfig, curConfig *v1beta1.ValidatingWebhookConfiguration
	var err error
	client := k8sClient.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()
	curConfig, err = client.Get(context.TODO(), config.Name, metav1.GetOptions{})
	if err == nil {
		fmt.Fprintf(writer, "update webhook configuration %v\n", config.Name)
		config.ObjectMeta.ResourceVersion = curConfig.ObjectMeta.ResourceVersion
		whConfig, err = client.Update(context.TODO(), config, metav1.UpdateOptions{})
	} else {
		fmt.Fprintf(writer, "create webhook configuration %v\n", config.Name)
		whConfig, err = client.Create(context.TODO(), config, metav1.CreateOptions{})
	}
	return whConfig, err
}

// Create the mutatingwebhookconfiguration
func createMutatingWebhookConfig(k8sClient kubernetes.Interface,
	config *v1beta1.MutatingWebhookConfiguration, writer io.Writer) (*v1beta1.MutatingWebhookConfiguration, error) {
	var curConfig, whConfig *v1beta1.MutatingWebhookConfiguration
	var err error
	client := k8sClient.AdmissionregistrationV1beta1().MutatingWebhookConfigurations()
	curConfig, err = client.Get(context.TODO(), config.Name, metav1.GetOptions{})
	if err == nil {
		fmt.Fprintf(writer, "update webhook configuration %v\n", config.Name)
		config.ObjectMeta.ResourceVersion = curConfig.ObjectMeta.ResourceVersion
		whConfig, err = client.Update(context.TODO(), config, metav1.UpdateOptions{})
	} else {
		fmt.Fprintf(writer, "create webhook configuration %v\n", config.Name)
		whConfig, err = client.Create(context.TODO(), config, metav1.CreateOptions{})
	}
	return whConfig, err
}

// Enable webhook configurations
func enableWebhookConfig(client kubernetes.Interface, opt *enableCliOptions, writer io.Writer) error {
	var errRet error

	// Read Kubernetes CA certificate that will be used as the CA bundle of the webhook configurations
	caCert, err := readCACert(client, opt.caCertPath, opt.webhookSecretName, opt.webhookSecretNameSpace,
		opt.maxTimeForReadingWebhookCert, writer)
	if err != nil {
		errRet = multierror.Append(errRet, fmt.Errorf("err when reading CA certificate: %v", err))
		return errRet
	}
	fmt.Fprintf(writer, "Webhook CA certificate:\n%v\n", string(caCert))

	if opt.enableValidationWebhook {
		err := enableValidationWebhookConfig(client, caCert, opt, writer)
		if err != nil {
			errRet = multierror.Append(errRet, err)
		}
	}
	if opt.enableMutationWebhook {
		err := enableMutationWebhookConfig(client, caCert, opt, writer)
		if err != nil {
			errRet = multierror.Append(errRet, err)
		}
	}
	return errRet
}

// Enable validation webhook configuration
func enableValidationWebhookConfig(client kubernetes.Interface, caCert []byte, opt *enableCliOptions, writer io.Writer) error {
	// Apply the webhook configuration when the Galley webhook server is running.
	err := waitForServerRunning(client, namespace, opt.validatingWebhookServiceName,
		opt.maxTimeForCheckingWebhookServer, writer)
	if err != nil {
		return fmt.Errorf("err when checking validation webhook server: %v", err)
	}
	webhookConfig, err := buildValidatingWebhookConfig(caCert, opt.validationWebhookConfigPath)
	if err != nil {
		return fmt.Errorf("err when build validatingwebhookconfiguration: %v", err)
	}
	_, err = createValidatingWebhookConfig(client, webhookConfig, writer)
	if err != nil {
		return fmt.Errorf("error when creating validatingwebhookconfiguration: %v", err)
	}
	return nil
}

// Enable mutation webhook configuration
func enableMutationWebhookConfig(client kubernetes.Interface, caCert []byte, opt *enableCliOptions, writer io.Writer) error {
	// Apply the webhook configuration when the Sidecar Injector webhook server is running.
	err := waitForServerRunning(client, namespace, opt.mutatingWebhookServiceName,
		opt.maxTimeForCheckingWebhookServer, writer)
	if err != nil {
		return fmt.Errorf("err when checking injection webhook server: %v", err)
	}
	webhookConfig, err := buildMutatingWebhookConfig(caCert, opt.mutatingWebhookConfigPath)
	if err != nil {
		return fmt.Errorf("err when build mutatingwebhookconfiguration: %v", err)
	}
	_, err = createMutatingWebhookConfig(client, webhookConfig, writer)
	if err != nil {
		return fmt.Errorf("error when creating mutatingwebhookconfiguration: %v", err)
	}
	return nil
}

// Disable webhook configurations
func disableWebhookConfig(k8sClient kubernetes.Interface, opt *disableCliOptions) (error, error) {
	var validationErr error
	var injectionErr error
	if opt.disableValidationWebhook {
		validationErr = k8sClient.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().
			Delete(context.TODO(), opt.validatingWebhookConfigName, metav1.DeleteOptions{})
	}
	if opt.disableInjectionWebhook {
		injectionErr = k8sClient.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().
			Delete(context.TODO(), opt.mutatingWebhookConfigName, metav1.DeleteOptions{})
	}
	return validationErr, injectionErr
}

// Display webhook configurations
func displayWebhookConfig(client kubernetes.Interface, opt *statusCliOptions, writer io.Writer) (error, error) {
	var validationErr error
	var injectionErr error
	if opt.validationWebhook {
		validationErr = displayValidationWebhookConfig(client, opt, writer)
	}
	if opt.injectionWebhook {
		injectionErr = displayMutationWebhookConfig(client, opt, writer)
	}
	return validationErr, injectionErr
}

// Display validation webhook configuration
func displayValidationWebhookConfig(client kubernetes.Interface, opt *statusCliOptions, writer io.Writer) error {
	config, err := client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Get(
		context.TODO(),
		opt.validatingWebhookConfigName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error getting ValidatingWebhookConfiguration %v: %v", opt.validatingWebhookConfigName, err)
	}
	b, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("error when marshaling ValidatingWebhookConfiguration %v to yaml: %v",
			opt.validatingWebhookConfigName, err)
	}
	fmt.Fprintf(writer, "ValidatingWebhookConfiguration %v is:\n%v\n", opt.validatingWebhookConfigName, string(b))
	return nil
}

// Display mutation webhook configuration
func displayMutationWebhookConfig(client kubernetes.Interface, opt *statusCliOptions, writer io.Writer) error {
	config, err := client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Get(
		context.TODO(),
		opt.mutatingWebhookConfigName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error getting MutatingWebhookConfiguration %v: %v", opt.mutatingWebhookConfigName, err)
	}
	b, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("error when marshaling MutatingWebhookConfiguration %v to yaml: %v",
			opt.mutatingWebhookConfigName, err)
	}
	fmt.Fprintf(writer, "MutatingWebhookConfiguration %v is:\n%v\n", opt.mutatingWebhookConfigName, string(b))
	return nil
}

// Read CA certificate and check whether it is a valid certificate.
// First try read CA cert from a local file. If no local file, read from CA cert from default service account.
// Next verify that the webhook certificate is issued by CA cert.
func readCACert(client kubernetes.Interface, certPath, secretName, secretNamespace string,
	maxWaitTime time.Duration, writer io.Writer) ([]byte, error) {
	var caCert []byte
	var err error
	if len(certPath) > 0 { // read the CA certificate from a local file
		caCert, err = ioutil.ReadFile(certPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert, cert. path: %v, error: %v", certPath, err)
		}
	} else {
		// Read the CA certificate from the default service account
		caCert, err = readCACertFromSA(client, caCertServiceAccountNamespace, caCertServiceAccount)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert from %v/%v, error: %v",
				caCertServiceAccountNamespace, caCertServiceAccount, err)
		}
	}
	if err = checkCertificate(caCert); err != nil {
		return nil, fmt.Errorf("ca certificate is invalid: %v", err)
	}

	// Read the webhook certificate and check whether it is issued by CA cert
	startTime := time.Now()
	timerCh := time.After(maxWaitTime - time.Since(startTime))
	var cert []byte
	for {
		cert, err = readCertFromSecret(client, secretName, secretNamespace)
		if err == nil {
			fmt.Fprintf(writer, "finished reading cert %v/%v\n", secretNamespace, secretName)
			break
		}
		fmt.Fprintf(writer, "could not read secret %v/%v: %v\n", secretNamespace, secretName, err)
		select {
		case <-timerCh:
			return nil, fmt.Errorf("the secret %v/%v is not readable within %v", secretNamespace, secretName, maxWaitTime)
		default:
			time.Sleep(2 * time.Second)
		}
	}
	if err = checkCertificate(cert); err != nil {
		return nil, fmt.Errorf("webhook certificate is invalid: %v", err)
	}
	if err = veriyCertChain(cert, caCert); err != nil {
		return nil, fmt.Errorf("kubernetes CA cert and the webhook cert do not form a valid cert chain. "+
			"if your cluster has a custom Kubernetes signing cert, please specify it in --ca-bundle-file parameter. error: %v", err)
	}

	return caCert, nil
}

func waitForServerRunning(client kubernetes.Interface, namespace, svc string, maxWaitTime time.Duration, writer io.Writer) error {
	startTime := time.Now()
	timerCh := time.After(maxWaitTime - time.Since(startTime))
	// TODO (lei-tang): the retry here may be implemented through another retry mechanism.
	for {
		// Check the webhook's endpoint to see if it is ready. The webhook's readiness probe reflects the readiness of
		// its https server.
		err := isEndpointReady(client, svc, namespace)
		if err == nil {
			fmt.Fprintf(writer, "the server at %v/%v is running\n", namespace, svc)
			break
		}
		fmt.Fprintf(writer, "the server at %v/%v is not running: %v\n", namespace, svc, err)
		select {
		case <-timerCh:
			return fmt.Errorf("the server at %v/%v is not running within %v", namespace, svc, maxWaitTime)
		default:
			time.Sleep(2 * time.Second)
		}
	}
	return nil
}

// Build the desired validatingwebhookconfiguration from the specified CA
// and webhook config file.
func buildValidatingWebhookConfig(
	caCert []byte, webhookConfigPath string) (*v1beta1.ValidatingWebhookConfiguration, error) {
	// load and validate configuration
	webhookConfigData, err := ioutil.ReadFile(webhookConfigPath)
	if err != nil {
		return nil, err
	}
	var webhookConfig v1beta1.ValidatingWebhookConfiguration
	if err := yaml.Unmarshal(webhookConfigData, &webhookConfig); err != nil {
		return nil, fmt.Errorf("could not decode validatingwebhookconfiguration from %v: %v",
			webhookConfigPath, err)
	}

	// patch the ca-cert into the user provided configuration
	for i := range webhookConfig.Webhooks {
		webhookConfig.Webhooks[i].ClientConfig.CABundle = caCert
	}

	return &webhookConfig, nil
}

// Build the desired mutatingwebhookconfiguration from the specified CA
// and webhook config file.
func buildMutatingWebhookConfig(
	caCert []byte, webhookConfigPath string) (*v1beta1.MutatingWebhookConfiguration, error) {
	// load and validate configuration
	webhookConfigData, err := ioutil.ReadFile(webhookConfigPath)
	if err != nil {
		return nil, err
	}
	var webhookConfig v1beta1.MutatingWebhookConfiguration
	if err := yaml.Unmarshal(webhookConfigData, &webhookConfig); err != nil {
		return nil, fmt.Errorf("could not decode mutatingwebhookconfiguration from %v: %v",
			webhookConfigPath, err)
	}

	// patch the ca-cert into the user provided configuration
	for i := range webhookConfig.Webhooks {
		webhookConfig.Webhooks[i].ClientConfig.CABundle = caCert
	}

	return &webhookConfig, nil
}

// Return nil when the endpoint is ready. Otherwise, return an error.
func isEndpointReady(client kubernetes.Interface, svc, namespace string) error {
	ep, err := client.CoreV1().Endpoints(namespace).Get(context.TODO(), svc, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if len(ep.Subsets) == 0 {
		return fmt.Errorf("%v/%v endpoint not ready: no subsets", namespace, svc)
	}
	for _, subset := range ep.Subsets {
		if len(subset.Addresses) > 0 {
			return nil
		}
	}
	return fmt.Errorf("%v/%v endpoint not ready: no subset addresses", namespace, svc)
}

// Read secret of a service account.
func readCACertFromSA(client kubernetes.Interface, namespace, svcAcctName string) ([]byte, error) {
	svcAcct, err := client.CoreV1().ServiceAccounts(namespace).Get(context.TODO(), svcAcctName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if len(svcAcct.Secrets) == 0 {
		return nil, fmt.Errorf("%v/%v has no secrets", namespace, svcAcctName)
	}
	secretName := svcAcct.Secrets[0].Name
	secret, err := client.CoreV1().Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, fmt.Errorf("secret %v/%v is nil", namespace, secretName)
	}
	d, ok := secret.Data[caKeyInK8sSecret]
	if !ok {
		return nil, fmt.Errorf("secret %v/%v does not contain %v", namespace, secretName, caKeyInK8sSecret)
	}
	return d, nil
}

// Read certificate from secret
func readCertFromSecret(client kubernetes.Interface, name, namespace string) ([]byte, error) {
	secret, err := client.CoreV1().Secrets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, fmt.Errorf("secret %v/%v is nil", namespace, name)
	}
	d, ok := secret.Data[certKeyInK8sSecret]
	if !ok {
		return nil, fmt.Errorf("secret %v/%v does not contain %v", namespace, name, certKeyInK8sSecret)
	}
	return d, nil
}

// Return nil if certificate is valid. Otherwise, return an error.
func checkCertificate(cert []byte) error {
	b, _ := pem.Decode(cert)
	if b == nil {
		return fmt.Errorf("could not decode pem")
	}
	if b.Type != "CERTIFICATE" {
		return fmt.Errorf("ca certificate contains wrong type: %v", b.Type)
	}
	if _, err := x509.ParseCertificate(b.Bytes); err != nil {
		return fmt.Errorf("ca certificate parsing returns an error: %v", err)
	}
	return nil
}

// Verify the cert is issued by the CA cert.
// Return nil if the cert is issued by the CA cert. Otherwise, return an error.
func veriyCertChain(cert, caCert []byte) error {
	roots := x509.NewCertPool()
	if roots == nil {
		return fmt.Errorf("failed to create cert pool")
	}
	if ok := roots.AppendCertsFromPEM(caCert); !ok {
		return fmt.Errorf("failed to append CA certificate")
	}
	b, _ := pem.Decode(cert)
	if b == nil {
		return fmt.Errorf("invalid PEM encoded certificate")
	}
	certParsed, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse X.509 certificate")
	}
	_, err = certParsed.Verify(x509.VerifyOptions{
		Roots: roots,
	})
	if err != nil {
		return fmt.Errorf("failed to verify the certificate chain: %v", err)
	}
	return nil
}
