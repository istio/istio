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

package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"istio.io/istio/pilot/cmd"
	"istio.io/istio/pilot/pkg/kube/inject"
	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
	"istio.io/istio/pkg/util"
	"istio.io/istio/pkg/version"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	sidecarWebhookConfigName = "istio-sidecar-injector"
	sidecarWebhookName       = "sidecar-injector.istio.io"
)

var (
	flags = struct {
		loggingOptions *log.Options

		meshconfig          string
		injectConfigFile    string
		certFile            string
		privateKeyFile      string
		caCertFile          string
		port                int
		healthCheckInterval time.Duration
		healthCheckFile     string
		probeOptions        probe.Options
		kubeConfigFile      string
	}{
		loggingOptions: log.DefaultOptions(),
	}

	rootCmd = &cobra.Command{
		Use:   "sidecar-injector",
		Short: "Kubernetes webhook for automatic Istio sidecar injection",
		RunE: func(*cobra.Command, []string) error {
			if err := log.Configure(flags.loggingOptions); err != nil {
				return err
			}

			log.Infof("version %s", version.Info.String())

			parameters := inject.WebhookParameters{
				ConfigFile:          flags.injectConfigFile,
				MeshFile:            flags.meshconfig,
				CertFile:            flags.certFile,
				KeyFile:             flags.privateKeyFile,
				Port:                flags.port,
				HealthCheckInterval: flags.healthCheckInterval,
				HealthCheckFile:     flags.healthCheckFile,
			}
			wh, err := inject.NewWebhook(parameters)
			if err != nil {
				return multierror.Prefix(err, "failed to create injection webhook")
			}

			if err := patchCert(); err != nil {
				return multierror.Prefix(err, "failed to patch webhook config")
			}

			stop := make(chan struct{})
			go wh.Run(stop)
			cmd.WaitSignal(stop)
			return nil
		},
	}

	probeCmd = &cobra.Command{
		Use:   "probe",
		Short: "Check the liveness or readiness of a locally-running server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !flags.probeOptions.IsValid() {
				return errors.New("some options are not valid")
			}
			if err := probe.NewFileClient(&flags.probeOptions).GetStatus(); err != nil {
				return fmt.Errorf("fail on inspecting path %s: %v", flags.probeOptions.Path, err)
			}
			fmt.Println("OK")
			return nil
		},
	}
)

func patchCert() error {
	client, err := createClientset(flags.kubeConfigFile)
	if err != nil {
		return err
	}
	caCertPem, err := ioutil.ReadFile(flags.caCertFile)
	if err != nil {
		return err
	}
	err = util.PatchMutatingWebhookConfig(client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations(),
		sidecarWebhookConfigName, sidecarWebhookName, caCertPem)
	for err != nil {
		log.Errorf("Register webhook failed: %s. Retrying...", err)
		time.Sleep(time.Second * 10)
		err = util.PatchMutatingWebhookConfig(client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations(),
			sidecarWebhookConfigName, sidecarWebhookName, caCertPem)
	}
	return nil
}

func createClientset(kubeConfigFile string) (*kubernetes.Clientset, error) {
	var err error
	var c *rest.Config
	if kubeConfigFile != "" {
		c, err = clientcmd.BuildConfigFromFlags("", kubeConfigFile)
	} else {
		c, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(c)
	if err != nil {
		return nil, err
	}
	return cs, nil
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flags.meshconfig, "meshConfig", "/etc/istio/config/mesh",
		"File containing the Istio mesh configuration")
	rootCmd.PersistentFlags().StringVar(&flags.injectConfigFile, "injectConfig", "/etc/istio/inject/config",
		"File containing the Istio sidecar injection configuration and template")
	rootCmd.PersistentFlags().StringVar(&flags.certFile, "tlsCertFile", "/etc/istio/certs/cert-chain.pem",
		"File containing the x509 Certificate for HTTPS.")
	rootCmd.PersistentFlags().StringVar(&flags.privateKeyFile, "tlsKeyFile", "/etc/istio/certs/key.pem",
		"File containing the x509 private key matching --tlsCertFile.")
	rootCmd.PersistentFlags().StringVar(&flags.caCertFile, "caCertFile", "/etc/istio/certs/root-cert.pem",
		"File containing the x509 Certificate for HTTPS.")
	rootCmd.PersistentFlags().IntVar(&flags.port, "port", 443, "Webhook port")

	rootCmd.PersistentFlags().DurationVar(&flags.healthCheckInterval, "healthCheckInterval", 0,
		"Configure how frequently the health check file specified by --healhCheckFile should be updated")
	rootCmd.PersistentFlags().StringVar(&flags.healthCheckFile, "healthCheckFile", "",
		"File that should be periodically updated if health checking is enabled")
	rootCmd.PersistentFlags().StringVar(&flags.kubeConfigFile, "kube-config", "",
		"Specifies path to kubeconfig file. This must be specified when not running inside a Kubernetes pod.")
	// Attach the Istio logging options to the command.
	flags.loggingOptions.AttachCobraFlags(rootCmd)

	cmd.AddFlags(rootCmd)

	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Sidecar Injector",
		Section: "sidecar-injector CLI",
		Manual:  "Istio Sidecar Injector",
	}))

	probeCmd.PersistentFlags().StringVar(&flags.probeOptions.Path, "probe-path", "",
		"Path of the file for checking the availability.")
	probeCmd.PersistentFlags().DurationVar(&flags.probeOptions.UpdateInterval, "interval", 0,
		"Duration used for checking the target file's last modified time.")
	rootCmd.AddCommand(probeCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}
