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

// Package mesh contains types and functions that are used across the full
// set of mixer commands.
package mesh

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/cache"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/pkg/log"
)

var (
	// installerScope is the scope for all commands in the mesh package.
	installerScope = log.RegisterScope("installer", "installer", 0)

	// testK8Interface is used if it is set. Not possible to inject due to cobra command boundary.
	testK8Interface *kubernetes.Clientset
	testRestConfig  *rest.Config
)

func initLogsOrExit(_ *rootArgs) {
	if err := configLogs(log.DefaultOptions()); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Could not configure logs: %s", err)
		os.Exit(1)
	}
}

func configLogs(opt *log.Options) error {
	op := []string{"stderr"}
	opt2 := *opt
	opt2.OutputPaths = op
	opt2.ErrorOutputPaths = op

	return log.Configure(&opt2)
}

func refreshGoldenFiles() bool {
	ev := os.Getenv("REFRESH_GOLDEN")
	return ev == "true" || ev == "1"
}

func kubeBuilderInstalled() bool {
	ev := os.Getenv("KUBEBUILDER")
	return ev == "true" || ev == "1"
}

// confirm waits for a user to confirm with the supplied message.
func confirm(msg string, writer io.Writer) bool {
	fmt.Fprintf(writer, "%s ", msg)

	var response string
	_, err := fmt.Scanln(&response)
	if err != nil {
		return false
	}
	response = strings.ToUpper(response)
	if response == "Y" || response == "YES" {
		return true
	}

	return false
}

// K8sConfig creates a rest.Config, Clientset and controller runtime Client from the given kubeconfig path and context.
func K8sConfig(kubeConfigPath string, context string) (*rest.Config, *kubernetes.Clientset, client.Client, error) {
	restConfig, clientset, err := InitK8SRestClient(kubeConfigPath, context)
	if err != nil {
		return nil, nil, nil, err
	}
	// We are running a one-off command locally, so we don't need to worry too much about rate limitting
	// Bumping this up greatly decreases install time
	restConfig.QPS = 50
	restConfig.Burst = 100
	client, err := client.New(restConfig, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, nil, nil, err
	}
	return restConfig, clientset, client, nil
}

// InitK8SRestClient creates a rest.Config qne Clientset from the given kubeconfig path and context.
func InitK8SRestClient(kubeconfig, kubeContext string) (*rest.Config, *kubernetes.Clientset, error) {
	if testRestConfig != nil || testK8Interface != nil {
		if !(testRestConfig != nil && testK8Interface != nil) {
			return nil, nil, fmt.Errorf("testRestConfig and testK8Interface must both be either nil or set")
		}
		return testRestConfig, testK8Interface, nil
	}
	restConfig, err := defaultRestConfig(kubeconfig, kubeContext)
	if err != nil {
		return nil, nil, err
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, err
	}

	return restConfig, clientset, nil
}

func defaultRestConfig(kubeconfig, kubeContext string) (*rest.Config, error) {
	config, err := BuildClientConfig(kubeconfig, kubeContext)
	if err != nil {
		return nil, err
	}
	config.APIPath = "/api"
	config.GroupVersion = &v1.SchemeGroupVersion
	config.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}
	return config, nil
}

// BuildClientConfig is a helper function that builds client config from a kubeconfig filepath.
// It overrides the current context with the one provided (empty to use default).
//
// This is a modified version of k8s.io/client-go/tools/clientcmd/BuildConfigFromFlags with the
// difference that it loads default configs if not running in-cluster.
func BuildClientConfig(kubeconfig, context string) (*rest.Config, error) {
	if kubeconfig != "" {
		info, err := os.Stat(kubeconfig)
		if err != nil || info.Size() == 0 {
			// If the specified kubeconfig doesn't exists / empty file / any other error
			// from file stat, fall back to default
			kubeconfig = ""
		}
	}

	//Config loading rules:
	// 1. kubeconfig if it not empty string
	// 2. In cluster config if running in-cluster
	// 3. Config(s) in KUBECONFIG environment variable
	// 4. Use $HOME/.kube/config
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.DefaultClientConfig = &clientcmd.DefaultClientConfig
	loadingRules.ExplicitPath = kubeconfig
	configOverrides := &clientcmd.ConfigOverrides{
		ClusterDefaults: clientcmd.ClusterDefaults,
		CurrentContext:  context,
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
}

// applyOptions contains the startup options for applying the manifest.
type applyOptions struct {
	// Path to the kubeconfig file.
	Kubeconfig string
	// ComponentName of the kubeconfig context to use.
	Context string
	// DryRun performs all steps except actually applying the manifests or creating output dirs/files.
	DryRun bool
	// Maximum amount of time to wait for resources to be ready after install when Wait=true.
	WaitTimeout time.Duration
}

func applyManifest(restConfig *rest.Config, client client.Client, manifestStr string,
	componentName name.ComponentName, opts *applyOptions, iop *v1alpha1.IstioOperator, l clog.Logger) error {
	// Needed in case we are running a test through this path that doesn't start a new process.
	cache.FlushObjectCaches()
	reconciler, err := helmreconciler.NewHelmReconciler(client, restConfig, iop, &helmreconciler.Options{DryRun: opts.DryRun, Log: l})
	if err != nil {
		l.LogAndError(err)
		return err
	}
	ms := name.Manifest{
		Name:    componentName,
		Content: manifestStr,
	}
	_, _, err = reconciler.ApplyManifest(ms)
	return err
}

// --manifests is an alias for --set installPackagePath=
// --revision is an alias for --set revision=
func applyFlagAliases(flags []string, manifestsPath, revision string) []string {
	if manifestsPath != "" {
		flags = append(flags, fmt.Sprintf("installPackagePath=%s", manifestsPath))
	}
	if revision != "" {
		flags = append(flags, fmt.Sprintf("revision=%s", revision))
	}
	return flags
}

// getCRAndNamespaceFromFile returns the CR name and istio namespace from a file containing an IstioOperator CR.
func getCRAndNamespaceFromFile(filePath string, l clog.Logger) (customResource string, istioNamespace string, err error) {
	if filePath == "" {
		return "", "", nil
	}

	_, mergedIOPS, err := manifest.GenerateConfig([]string{filePath}, nil, false, nil, l)
	if err != nil {
		return "", "", err
	}

	b, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", "", fmt.Errorf("could not read values from file %s: %s", filePath, err)
	}
	customResource = string(b)
	istioNamespace = v1alpha1.Namespace(mergedIOPS)
	return
}

// createNamespace creates a namespace using the given k8s interface.
func createNamespace(cs kubernetes.Interface, namespace string) error {
	if namespace == "" {
		// Setup default namespace
		namespace = "istio-system"
	}

	ns := &v1.Namespace{ObjectMeta: v12.ObjectMeta{
		Name: namespace,
		Labels: map[string]string{
			"istio-injection": "disabled",
		},
	}}
	_, err := cs.CoreV1().Namespaces().Create(context.TODO(), ns, v12.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create namespace %v: %v", namespace, err)
	}
	return nil
}

// saveIOPToCluster saves the state in an IOP CR in the cluster.
func saveIOPToCluster(reconciler *helmreconciler.HelmReconciler, iop string) error {
	obj, err := object.ParseYAMLToK8sObject([]byte(iop))
	if err != nil {
		return err
	}
	return reconciler.ApplyObject(obj.UnstructuredObject())
}
