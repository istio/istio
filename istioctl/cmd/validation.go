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
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os/exec"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/spf13/cobra"

	"k8s.io/api/admissionregistration/v1beta1"

	"github.com/ghodss/yaml"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	k8slabels "k8s.io/apimachinery/pkg/labels"
)

const (
	nameForTheTempPod           = "temporary-pod-created-for-reading-k8s-ca-cert"
	labelKeyForTheTempPod       = "temporary-pod-created-for-reading-k8s-ca-cert"
	labelValForTheTempPod       = "true"
	namespaceForTheTempPod      = "default"
	containerNameForTheTempPod  = "sleep"
	containerImageForTheTempPod = "governmentpaas/curl-ssl"
	maxPodWaitDuration          = 60 * time.Second
)

var (
	opts                      = cliOptions{}
	containerCmdForTheTempPod = []string{"/bin/sleep", "3650d"}
)

type cliOptions struct {
	// Read the webhook CA certificate from local file system
	readCaCertFromLocalFile bool
	// The file path of webhook CA certificate
	caCertPath string
	// The file path of the webhook configuration.
	webhookConfigPath string
	// The name of the ValidatingWebhookConfiguration to manage
	validatingWebhookConfigName string
	// The service name of the validating webhook to manage
	validatingWebhookServiceName string
	// The service port of the validating webhook to manage
	validatingWebhookServicePort int
	// The namespace of the validating webhook to manage
	validatingWebhookNamespace string
	// Max time (in seconds) for checking the validating webhook.
	// If the validating webhook is not ready in the given time, exit.
	// Otherwise, apply the webhook configuration.
	maxTimeForCheckingWebhookServer int
}

// Validation command for Galley webhook configuration
func Validation() *cobra.Command {
	validationCmd := &cobra.Command{
		Use:   "validation",
		Short: "validation commands for Galley webhook configuration",
	}

	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable the webhook configuration of Galley",
		Example: `
# Enable the webhook configuration of Galley
istioctl experimental validation enable

istioctl experimental validation enable --webhook-config-path /etc/galley/validatingwebhookconfiguration.yaml

istioctl experimental validation enable --webhook-config-path /etc/galley/validatingwebhookconfiguration.yaml 
  --ca-cert-path /var/run/secrets/kubernetes.io/serviceaccount/ca.crt

istioctl experimental validation enable --webhook-config-path /etc/galley/validatingwebhookconfiguration.yaml
  --ca-cert-path ./k8s-ca-cert.pem --read-ca-cert-from-local-file true
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := createInterface(kubeconfig)
			if err != nil {
				return err
			}
			// Read k8s CA certificate that will be used as the CA bundle of the webhook configuration
			caCert, err := readCACert(client, opts.caCertPath, opts.readCaCertFromLocalFile)
			if err != nil {
				if !opts.readCaCertFromLocalFile {
					if delErr := deletePod(client, nameForTheTempPod, namespaceForTheTempPod); delErr != nil {
						fmt.Printf("when deleting pod %v, return: %v\n", nameForTheTempPod, delErr)
					}
				}
				fmt.Printf("err when reading CA cert: %v\n", err)
				return err
			}
			fmt.Printf("Webhook CA certificate:\n%v\n", string(caCert))

			// Apply the webhook configuration when the Galley webhook server is running.
			hostValidate := fmt.Sprintf("%s.%s", opts.validatingWebhookServiceName, opts.validatingWebhookNamespace)
			err = waitForServerRunning(hostValidate, opts.validatingWebhookServicePort,
				time.Duration(opts.maxTimeForCheckingWebhookServer)*time.Second)
			if !opts.readCaCertFromLocalFile {
				if delErr := deletePod(client, nameForTheTempPod, namespaceForTheTempPod); delErr != nil {
					fmt.Printf("when deleting pod %v, return: %v\n", nameForTheTempPod, delErr)
				}
			}
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
	validationCmd.AddCommand(cmd)

	flags := cmd.Flags()
	flags.BoolVar(&opts.readCaCertFromLocalFile, "read-ca-cert-from-local-file", false, "Specifies whether reading"+
		"CA certificate from local file system or not.")
	// Specifies the file path to webhook CA certificate.
	// The default value is configured based on https://kubernetes.io/docs/tasks/tls/managing-tls-in-a-cluster/:
	// /var/run/secrets/kubernetes.io/serviceaccount/ca.crt.
	flags.StringVar(&opts.caCertPath, "ca-cert-path", "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
		"Specifies the file path to webhook CA certificate.")
	flags.StringVar(&opts.webhookConfigPath, "webhook-config-path", "/etc/galley/validatingwebhookconfiguration.yaml",
		"Specifies the file path of the webhook configuration.")
	flags.StringVar(&opts.validatingWebhookConfigName, "validating-webhook-config-name", "istio-galley",
		"The name of the ValidatingWebhookConfiguration to manage.")
	flags.StringVar(&opts.validatingWebhookServiceName, "validating-webhook-service-name", "istio-galley",
		"The service name of the validating webhook to manage.")
	flags.IntVar(&opts.validatingWebhookServicePort, "validating-webhook-service-port", 443,
		"The service port of the validating webhook to manage.")
	flags.StringVar(&opts.validatingWebhookNamespace, "validating-webhook-namespace", "istio-system",
		"The namespace of the validating webhook to manage.")
	flags.IntVar(&opts.maxTimeForCheckingWebhookServer, "max-time-for-checking-webhook-server", 60,
		"	Max time (in seconds) for checking the validating webhook server. If the validating webhook server is not ready"+
			"in the given time, exit. Otherwise, apply the webhook configuration.")

	return validationCmd
}

// Create the validatingwebhookconfiguration
func createValidatingWebhookConfig(k8sClient kubernetes.Interface,
	config *v1beta1.ValidatingWebhookConfiguration) (*v1beta1.ValidatingWebhookConfiguration, error) {
	if config == nil {
		return nil, fmt.Errorf("validatingwebhookconfiguration is nil")
	}
	client := k8sClient.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()
	_, err := client.Get(config.Name, metav1.GetOptions{})
	if err == nil {
		// Delete the existing webhook configuration
		if delErr := client.Delete(config.Name, &metav1.DeleteOptions{}); delErr != nil {
			fmt.Printf("when deleting webhook configuration %v, return: %v\n", config.Name, delErr)
		}
		time.Sleep(5 * time.Second)
	}
	fmt.Printf("create webhook configuration %v\n", config.Name)
	whConfig, err := client.Create(config)
	return whConfig, err
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
		// Create a pod and wait it is ready before reading k8s CA certificate.
		// If the pod already exists, proceed to reading k8s CA certificate from the pod.
		if err = createPod(client, nameForTheTempPod, namespaceForTheTempPod, labelKeyForTheTempPod,
			labelValForTheTempPod, containerNameForTheTempPod, containerImageForTheTempPod,
			containerCmdForTheTempPod); err != nil && !errors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("error when creating pod and err is not already exists: %v", err)
		}
		fmt.Printf("pod %v created\n", nameForTheTempPod)
		fmt.Printf("waiting for at most %v until the pod is running ...\n", maxPodWaitDuration)
		err = waitForPodRunning(client, nameForTheTempPod, labelKeyForTheTempPod, labelValForTheTempPod,
			namespaceForTheTempPod, maxPodWaitDuration)
		if err != nil {
			return nil, fmt.Errorf("error when waiting for the pod: %v", err)
		}
		execCmd := "cat " + certPath
		execOutput, err := podExec(namespaceForTheTempPod, nameForTheTempPod, containerNameForTheTempPod,
			execCmd, true, kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("error when exec on the pod: %v", err)
		}
		if execOutput == "" {
			return nil, fmt.Errorf("empty pod exec output")
		}
		caCert = []byte(execOutput)
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

func createPod(k8sInterface kubernetes.Interface, podName, namespace, podLabelKey,
	podLabelVal, containerName, containerImage string, containerCmd []string) error {
	req := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   podName,
			Labels: map[string]string{podLabelKey: podLabelVal},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    containerName,
					Image:   containerImage,
					Command: containerCmd,
				},
			},
		},
	}
	resp, err := k8sInterface.CoreV1().Pods(namespace).Create(req)
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("pod creation response is nil")
	}
	return nil
}

func waitForServerRunning(host string, port int, maxWaitTime time.Duration) error {
	startTime := time.Now()
	timerCh := time.After(maxWaitTime - time.Since(startTime))
	for {
		err := isTLSServerRunning(host, port)
		if err == nil {
			fmt.Printf("the server at %v:%v is running\n", host, port)
			break
		}
		fmt.Printf("the server at %v:%v is not running: %v\n", host, port, err)
		select {
		case <-timerCh:
			return fmt.Errorf("server is not running within %v", maxWaitTime)
		default:
			time.Sleep(2 * time.Second)
		}
	}
	return nil
}

func waitForPodRunning(client kubernetes.Interface, podName, podLabelKey, podLabelVal, namespace string,
	maxWaitTime time.Duration) error {
	fieldSelector := fields.SelectorFromSet(map[string]string{
		"metadata.name":      podName,
		"metadata.namespace": namespace}).String()
	labelSelector := k8slabels.SelectorFromSet(map[string]string{podLabelKey: podLabelVal}).String()
	listOptions := metav1.ListOptions{
		FieldSelector: fieldSelector,
		LabelSelector: labelSelector,
	}
	watch, err := client.CoreV1().Pods(namespace).Watch(listOptions)
	if err != nil {
		return fmt.Errorf("failed to set up a watch for pod (error: %v)", err)
	}
	events := watch.ResultChan()

	startTime := time.Now()
	for {
		select {
		case event := <-events:
			pod := event.Object.(*corev1.Pod)
			if pod.Status.Phase == corev1.PodRunning {
				fmt.Printf("pod %v/%v is running\n", namespace, pod.GetName())
				return nil
			}
		case <-time.After(maxWaitTime - time.Since(startTime)):
			return fmt.Errorf("pod is not running within %v", maxWaitTime)
		}
	}
}

func deletePod(client kubernetes.Interface, podName, namespace string) error {
	err := client.CoreV1().Pods(namespace).Delete(podName, &metav1.DeleteOptions{})
	return err
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

// podExec runs the specified command on the container for the specified namespace and pod
func podExec(n, pod, container, command string, muteOutput bool, kubeconfig string) (string, error) {
	if muteOutput {
		return shellSilent("kubectl exec --kubeconfig=%s %s -n %s -c %s -- %s", kubeconfig, pod, n, container, command)
	}
	return shell("kubectl exec --kubeconfig=%s %s -n %s -c %s -- %s ", kubeconfig, pod, n, container, command)
}

// shell run command on shell and get back output and error if get one
func shell(format string, args ...interface{}) (string, error) {
	return sh(context.Background(), format, true, true, args...)
}

// shellSilent runs command on shell and get back output and error if get one
// without logging the command or output.
func shellSilent(format string, args ...interface{}) (string, error) {
	return sh(context.Background(), format, false, false, args...)
}

func sh(ctx context.Context, format string, logOutput, logError bool, args ...interface{}) (string, error) {
	command := fmt.Sprintf(format, args...)
	c := exec.CommandContext(ctx, "sh", "-c", command) // #nosec
	bytes, err := c.CombinedOutput()
	if logOutput {
		if output := strings.TrimSuffix(string(bytes), "\n"); len(output) > 0 {
			fmt.Printf("command output: \n%s\n", output)
		}
	}

	if err != nil {
		if logError {
			fmt.Printf("command error: %v\n", err)
		}
		return string(bytes), fmt.Errorf("command failed: %q %v", string(bytes), err)
	}
	return string(bytes), nil
}

// Return nil when TLS server is running. Otherwise, return error.
func isTLSServerRunning(host string, port int) error {
	sslCmd := fmt.Sprintf("openssl s_client -connect %s:%d -showcerts 2>&1", host, port)
	execCmd := `sh -c "echo | ` + sslCmd + ` | sed -ne '/^Server certificate/p'"`
	execOutput, err := podExec(namespaceForTheTempPod, nameForTheTempPod, containerNameForTheTempPod,
		execCmd, true, kubeconfig)
	if err != nil {
		return fmt.Errorf("error when exec on the pod: %v", err)
	}
	if !strings.HasPrefix(execOutput, "Server certificate") {
		return fmt.Errorf("TLS server certificate not found")
	}
	return nil
}
