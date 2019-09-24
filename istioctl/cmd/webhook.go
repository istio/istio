// Copyright 2019 Istio Authors
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
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/spf13/cobra"

	"k8s.io/api/admissionregistration/v1beta1"

	"github.com/ghodss/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	caCertServiceAccount          = "default"
	caCertServiceAccountNamespace = "default"
	caKeyInK8sSecret              = "ca.crt"
)

var (
	enableOpts  = enableCliOptions{}
	disableOpts = disableCliOptions{}
	statusOpts  = statusCliOptions{}
)

type enableCliOptions struct {
	// Whether enable the webhook configuration of Galley
	enableValidationWebhook bool
	// Read the webhook CA certificate from local file system
	readCaCertFromLocalFile bool
	// The local file path of webhook CA certificate
	caCertPath string
	// The file path of the webhook configuration.
	webhookConfigPath string
	// The name of the ValidatingWebhookConfiguration to manage
	validatingWebhookConfigName string
	// The service name of the validating webhook to manage
	validatingWebhookServiceName string
	// Max time (in seconds) for checking the validating webhook.
	// If the validating webhook is not ready in the given time, exit.
	// Otherwise, apply the webhook configuration.
	maxTimeForCheckingWebhookServer int
}

type disableCliOptions struct {
	// Whether disable the webhook configuration of Galley
	disableValidationWebhook bool
	// The name of the ValidatingWebhookConfiguration to manage
	validatingWebhookConfigName string
}

type statusCliOptions struct {
	// Whether display the webhook configuration of Galley
	validationWebhook bool
	// The name of the ValidatingWebhookConfiguration to manage
	validatingWebhookConfigName string
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
	opts := &enableOpts
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable webhook configurations",
		Example: `
# Enable the webhook configuration of Galley
istioctl experimental post-install webhook enable --validation --namespace istio-system

# Enable the webhook configuration of Galley with the given webhook configuration
istioctl experimental post-install webhook enable --validation --namespace istio-system --config-path /etc/galley/validatingwebhookconfiguration.yaml

# Enable the webhook configuration of Galley with the given webhook configuration and CA certificate
istioctl experimental post-install webhook enable --validation --namespace istio-system --config-path /etc/galley/validatingwebhookconfiguration.yaml
  --ca-cert-path ./k8s-ca-cert.pem --read-ca-cert-from-local-file true
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !opts.enableValidationWebhook {
				fmt.Println("not enabling validation webhook")
				return nil
			}
			client, err := createInterface(kubeconfig)
			if err != nil {
				fmt.Printf("err when creating k8s client interface: %v\n", err)
				return err
			}
			// Read k8s CA certificate that will be used as the CA bundle of the webhook configuration
			caCert, err := readCACert(client, opts.caCertPath, opts.readCaCertFromLocalFile)
			if err != nil {
				fmt.Printf("err when reading CA cert: %v\n", err)
				return err
			}
			fmt.Printf("Webhook CA certificate:\n%v\n", string(caCert))
			// Apply the webhook configuration when the Galley webhook server is running.
			err = waitForServerRunning(client, namespace, opts.validatingWebhookServiceName,
				time.Duration(opts.maxTimeForCheckingWebhookServer)*time.Second)
			if err != nil {
				fmt.Printf("err when checking webhook server: %v\n", err)
				return err
			}
			webhookConfig, err := buildValidatingWebhookConfig(
				caCert,
				opts.webhookConfigPath,
				opts.validatingWebhookConfigName,
			)
			if err != nil {
				fmt.Printf("err when build validatingwebhookconfiguration: %v\n", err)
				return err
			}
			whRet, err := createValidatingWebhookConfig(client, webhookConfig)
			if err != nil {
				fmt.Printf("error when creating validatingwebhookconfiguration: %v\n", err)
				return err
			}
			if whRet == nil {
				fmt.Println("validatingwebhookconfiguration created is nil")
				return fmt.Errorf("validatingwebhookconfiguration created is nil")
			}
			return nil
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&opts.enableValidationWebhook, "validation", true, "Specifies whether enabling"+
		"the validating webhook.")
	flags.BoolVar(&opts.readCaCertFromLocalFile, "read-ca-cert-from-local-file", false, "Specifies whether reading"+
		"CA certificate from local file system or not.")
	// Specifies the local file path to webhook CA certificate.
	// The default value is configured based on https://kubernetes.io/docs/tasks/tls/managing-tls-in-a-cluster/:
	// /var/run/secrets/kubernetes.io/serviceaccount/ca.crt.
	flags.StringVar(&opts.caCertPath, "ca-cert-path", "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
		"Specifies the local file path to webhook CA certificate.")
	flags.StringVar(&opts.webhookConfigPath, "config-path", "/etc/galley/validatingwebhookconfiguration.yaml",
		"Specifies the file path of the webhook configuration.")
	flags.StringVar(&opts.validatingWebhookConfigName, "config-name", "istio-galley",
		"The name of the ValidatingWebhookConfiguration to manage.")
	flags.StringVar(&opts.validatingWebhookServiceName, "service", "istio-galley",
		"The service name of the validating webhook to manage.")
	flags.IntVar(&opts.maxTimeForCheckingWebhookServer, "timeout", 60,
		"	Max time (in seconds) for checking the validating webhook server. If the validating webhook server is not ready"+
			"in the given time, exit. Otherwise, apply the webhook configuration.")

	return cmd
}

func newDisableCmd() *cobra.Command {
	opts := &disableOpts
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable webhook configurations",
		Example: `
# Disable the webhook configuration of Galley
istioctl experimental post-install webhook disable --validation --config-name istio-galley
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !opts.disableValidationWebhook {
				fmt.Println("not disabling validation webhook")
				return nil
			}
			client, err := createInterface(kubeconfig)
			if err != nil {
				fmt.Printf("err when creating k8s client interface: %v\n", err)
				return err
			}
			err = deleteValidatingWebhookConfig(client, opts.validatingWebhookConfigName)
			if err != nil {
				fmt.Printf("error when deleting validatingwebhookconfiguration: %v\n", err)
				return err
			}
			fmt.Printf("validatingwebhookconfiguration %v has been deleted\n", opts.validatingWebhookConfigName)
			return nil
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&opts.disableValidationWebhook, "validation", true, "Specifies whether disabling"+
		"the validating webhook.")
	flags.StringVar(&opts.validatingWebhookConfigName, "config-name", "istio-galley",
		"The name of the ValidatingWebhookConfiguration to disable.")

	return cmd
}

func newStatusCmd() *cobra.Command {
	opts := &statusOpts
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Get webhook configurations",
		Example: `
# Display the webhook configuration of Galley
istioctl experimental post-install webhook status --validation --config-name istio-galley
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !opts.validationWebhook {
				fmt.Println("not displaying validation webhook")
				return nil
			}
			client, err := createInterface(kubeconfig)
			if err != nil {
				fmt.Printf("err when creating k8s client interface: %v\n", err)
				return err
			}
			config, err := getValidatingWebhookConfig(client, opts.validatingWebhookConfigName)
			if err != nil {
				fmt.Printf("error getting validatingwebhookconfiguration: %v\n", err)
				return err
			}
			b, err := yaml.Marshal(config)
			if err != nil {
				fmt.Printf("error when marshaling validatingwebhookconfiguration to yaml: %v\n", err)
				return err
			}
			fmt.Printf("validatingwebhookconfiguration %v is:\n%v\n", opts.validatingWebhookConfigName, string(b))
			return nil
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&opts.validationWebhook, "validation", true, "Specifies whether displaying"+
		"the validating webhook configuration.")
	flags.StringVar(&opts.validatingWebhookConfigName, "config-name", "istio-galley",
		"The name of the ValidatingWebhookConfiguration to display.")

	return cmd
}

// Create the validatingwebhookconfiguration
func createValidatingWebhookConfig(k8sClient kubernetes.Interface,
	config *v1beta1.ValidatingWebhookConfiguration) (*v1beta1.ValidatingWebhookConfiguration, error) {
	var whConfig *v1beta1.ValidatingWebhookConfiguration
	var err error
	if config == nil {
		return nil, fmt.Errorf("validatingwebhookconfiguration is nil")
	}
	client := k8sClient.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()
	_, err = client.Get(config.Name, metav1.GetOptions{})
	if err == nil {
		fmt.Printf("update webhook configuration %v\n", config.Name)
		whConfig, err = client.Update(config)
	} else {
		fmt.Printf("create webhook configuration %v\n", config.Name)
		whConfig, err = client.Create(config)
	}
	return whConfig, err
}

// Delete the validatingwebhookconfiguration
func deleteValidatingWebhookConfig(k8sClient kubernetes.Interface, name string) error {
	client := k8sClient.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()
	err := client.Delete(name, &metav1.DeleteOptions{})
	return err
}

// Get the validatingwebhookconfiguration
func getValidatingWebhookConfig(k8sClient kubernetes.Interface, name string) (*v1beta1.ValidatingWebhookConfiguration, error) {
	client := k8sClient.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()
	return client.Get(name, metav1.GetOptions{})
}

// Read CA certificate and check whether it is a valid certificate.
func readCACert(client kubernetes.Interface, certPath string, readLocal bool) ([]byte, error) {
	var caCert []byte
	var err error
	if readLocal {
		caCert, err = ioutil.ReadFile(certPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert, cert. path: %v, error: %v", certPath, err)
		}
	} else {
		// Read CA certificate from default service account
		caCert, err = readSecret(client, caCertServiceAccountNamespace, caCertServiceAccount)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert from %v/%v, error: %v",
				caCertServiceAccountNamespace, caCertServiceAccount, err)
		}
	}
	b, _ := pem.Decode(caCert)
	if b == nil {
		return nil, fmt.Errorf("could not decode pem")
	}
	if b.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("ca certificate contains wrong type: %v", b.Type)
	}
	if _, err := x509.ParseCertificate(b.Bytes); err != nil {
		return nil, fmt.Errorf("ca certificate parsing returns an error: %v", err)
	}

	return caCert, nil
}

func waitForServerRunning(client kubernetes.Interface, namespace, svc string, maxWaitTime time.Duration) error {
	startTime := time.Now()
	timerCh := time.After(maxWaitTime - time.Since(startTime))
	for {
		// Check the webhook's endpoint to see if it is ready. The webhook's readiness probe reflects the readiness of
		// its https server.
		err := isEndpointReady(client, svc, namespace)
		if err == nil {
			fmt.Printf("the server at %v/%v is running\n", namespace, svc)
			break
		}
		fmt.Printf("the server at %v/%v is not running: %v\n", namespace, svc, err)
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
	caCert []byte, webhookConfigPath, webhookConfigName string,
) (*v1beta1.ValidatingWebhookConfiguration, error) {
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

	// the webhook name is fixed at startup time
	webhookConfig.Name = webhookConfigName

	// patch the ca-cert into the user provided configuration
	for i := range webhookConfig.Webhooks {
		webhookConfig.Webhooks[i].ClientConfig.CABundle = caCert
	}

	return &webhookConfig, nil
}

// Return nil when the endpoint is ready. Otherwise, return an error.
func isEndpointReady(client kubernetes.Interface, svc, namespace string) error {
	ep, err := client.CoreV1().Endpoints(namespace).Get(svc, metav1.GetOptions{})
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

// Return nil when the endpoint is ready. Otherwise, return an error.
func readSecret(client kubernetes.Interface, namespace, svcAcctName string) ([]byte, error) {
	svcAcct, err := client.CoreV1().ServiceAccounts(namespace).Get(svcAcctName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if len(svcAcct.Secrets) == 0 {
		return nil, fmt.Errorf("%v/%v has no secrets", namespace, svcAcctName)
	}
	secretName := svcAcct.Secrets[0].Name
	secret, err := client.CoreV1().Secrets(namespace).Get(secretName, metav1.GetOptions{})
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
