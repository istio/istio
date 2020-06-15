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

// A simple daemonset binary to repair pods that are crashlooping
// after winning a race condition against istio-cni
package main

import (
	"fmt"
	"os"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.uber.org/multierr"
	client "k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/cni/pkg/repair"
	"istio.io/pkg/log"
)

type ControllerOptions struct {
	RepairOptions *repair.Options `json:"repair_options"`
	RunAsDaemon   bool            `json:"run_as_daemon"`
}

var (
	loggingOptions = log.DefaultOptions()
)

// Parse command line options
func parseFlags() (filters *repair.Filters, options *ControllerOptions) {
	// Parse command line flags
	// Filter Options
	pflag.String("node-name", "", "The name of the managed node (will manage all nodes if unset)")
	pflag.String(
		"sidecar-annotation",
		"sidecar.istio.io/status",
		"An annotation key that indicates this pod contains an istio sidecar. All pods without this annotation will be ignored."+
			"The value of the annotation is ignored.")
	pflag.String(
		"init-container-name",
		"istio-validation",
		"The name of the istio init container (will crash-loop if CNI is not configured for the pod)")
	pflag.String(
		"init-container-termination-message",
		"",
		"The expected termination message for the init container when crash-looping because of CNI misconfiguration")
	pflag.Int(
		"init-container-exit-code",
		126,
		"Expected exit code for the init container when crash-looping because of CNI misconfiguration")

	pflag.String("label-selectors", "", "A set of label selectors in label=value format that will be added to the pod list filters")
	pflag.String("field-selectors", "", "A set of field selectors in label=value format that will be added to the pod list filters")

	// Repair Options
	pflag.Bool("delete-pods", false, "Controller will delete pods")
	pflag.Bool("label-pods", false, "Controller will label pods")
	pflag.Bool("run-as-daemon", false, "Controller will run in a loop")
	pflag.String(
		"broken-pod-label-key",
		"cni.istio.io/uninitialized",
		"The key portion of the label which will be set by the reconciler if --label-pods is true")
	pflag.String(
		"broken-pod-label-value",
		"true",
		"The value portion of the label which will be set by the reconciler if --label-pods is true")

	pflag.Bool("help", false, "Print usage information")

	pflag.Parse()
	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		log.Fatal("Error parsing command line args: %+v")
	}

	if viper.GetBool("help") {
		pflag.Usage()
		os.Exit(0)
	}

	viper.SetEnvPrefix("REPAIR")
	viper.AutomaticEnv()
	// Pull runtime args into structs
	filters = &repair.Filters{
		InitContainerName:               viper.GetString("init-container-name"),
		InitContainerTerminationMessage: viper.GetString("init-container-termination-message"),
		InitContainerExitCode:           viper.GetInt("init-container-exit-code"),
		SidecarAnnotation:               viper.GetString("sidecar-annotation"),
		FieldSelectors:                  viper.GetString("field-selectors"),
		LabelSelectors:                  viper.GetString("label-selectors"),
	}
	options = &ControllerOptions{
		RunAsDaemon: viper.GetBool("run-as-daemon"),
		RepairOptions: &repair.Options{
			DeletePods:    viper.GetBool("delete-pods"),
			LabelPods:     viper.GetBool("label-pods"),
			PodLabelKey:   viper.GetString("broken-pod-label-key"),
			PodLabelValue: viper.GetString("broken-pod-label-value"),
		},
	}

	if nodeName := viper.GetString("node-name"); nodeName != "" {
		filters.FieldSelectors = fmt.Sprintf("%s=%s,%s", "spec.nodeName", nodeName, filters.FieldSelectors)
	}

	return
}

// Set up Kubernetes client using kubeconfig (or in-cluster config if no file provided)
func clientSetup() (clientset *client.Clientset, err error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return
	}
	clientset, err = client.NewForConfig(config)
	return
}

// Log human-readable output describing the current filter and option selection
func logCurrentOptions(bpr *repair.BrokenPodReconciler, options *ControllerOptions) {
	if options.RunAsDaemon {
		log.Infof("Controller Option: Running as a Daemon.")
	}
	if bpr.Options.DeletePods {
		log.Info("Controller Option: Deleting broken pods. Pod Labeling deactivated.")
	}
	if bpr.Options.LabelPods && !bpr.Options.DeletePods {
		log.Infof(
			"Controller Option: Labeling broken pods with label %s=%s",
			bpr.Options.PodLabelKey,
			bpr.Options.PodLabelValue,
		)
	}
	if bpr.Filters.SidecarAnnotation != "" {
		log.Infof("Filter option: Only managing pods with an annotation with key %s", bpr.Filters.SidecarAnnotation)
	}
	if bpr.Filters.FieldSelectors != "" {
		log.Infof("Filter option: Only managing pods with field selector %s", bpr.Filters.FieldSelectors)
	}
	if bpr.Filters.LabelSelectors != "" {
		log.Infof("Filter option: Only managing pods with label selector %s", bpr.Filters.LabelSelectors)
	}
	if bpr.Filters.InitContainerName != "" {
		log.Infof("Filter option: Only managing pods where init container is named %s", bpr.Filters.InitContainerName)
	}
	if bpr.Filters.InitContainerTerminationMessage != "" {
		log.Infof("Filter option: Only managing pods where init container termination message is %s", bpr.Filters.InitContainerTerminationMessage)
	}
	if bpr.Filters.InitContainerExitCode != 0 {
		log.Infof("Filter option: Only managing pods where init container exit status is %d", bpr.Filters.InitContainerExitCode)
	}
}

func main() {
	loggingOptions.OutputPaths = []string{"stderr"}
	loggingOptions.JSONEncoding = true
	if err := log.Configure(loggingOptions); err != nil {
		os.Exit(1)
	}

	filters, options := parseFlags()

	clientSet, err := clientSetup()
	if err != nil {
		log.Fatalf("Could not construct clientSet: %s", err)
	}

	podFixer := repair.NewBrokenPodReconciler(clientSet, filters, options.RepairOptions)
	logCurrentOptions(&podFixer, options)

	if options.RunAsDaemon {
		rc, err := repair.NewRepairController(podFixer)
		if err != nil {
			log.Fatalf("Fatal error constructing repair controller: %+v", err)
		}
		stopCh := make(chan struct{})
		rc.Run(stopCh)

	} else {
		err = nil
		if podFixer.Options.LabelPods {
			err = multierr.Append(err, podFixer.LabelBrokenPods())
		}
		if podFixer.Options.DeletePods {
			err = multierr.Append(err, podFixer.DeleteBrokenPods())
		}
		if err != nil {
			log.Fatalf(err.Error())
		}
	}
}
