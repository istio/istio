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

// Package mesh contains types and functions.
package mesh

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/istio/istioctl/pkg/install/k8sversion"
	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/cache"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

var (
	// installerScope is the scope for all commands in the mesh package.
	installerScope = log.RegisterScope("installer", "installer", 0)

	// testK8Interface is used if it is set. Not possible to inject due to cobra command boundary.
	testK8Interface *kubernetes.Clientset
	testRestConfig  *rest.Config
)

type Printer interface {
	Printf(format string, a ...any)
	Println(string)
}

func NewPrinterForWriter(w io.Writer) Printer {
	return &writerPrinter{writer: w}
}

type writerPrinter struct {
	writer io.Writer
}

func (w *writerPrinter) Printf(format string, a ...any) {
	_, _ = fmt.Fprintf(w.writer, format, a...)
}

func (w *writerPrinter) Println(str string) {
	_, _ = fmt.Fprintln(w.writer, str)
}

func initLogsOrExit(_ *RootArgs) {
	if err := configLogs(log.DefaultOptions()); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Could not configure logs: %s", err)
		os.Exit(1)
	}
}

var logMutex = sync.Mutex{}

func configLogs(opt *log.Options) error {
	logMutex.Lock()
	defer logMutex.Unlock()
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
	for {
		_, _ = fmt.Fprintf(writer, "%s ", msg)
		var response string
		_, err := fmt.Scanln(&response)
		if err != nil {
			return false
		}
		switch strings.ToUpper(response) {
		case "Y", "YES":
			return true
		case "N", "NO":
			return false
		}
	}
}

func KubernetesClients(kubeConfigPath, context string, l clog.Logger) (kube.CLIClient, client.Client, error) {
	rc, err := kube.DefaultRestConfig(kubeConfigPath, context, func(config *rest.Config) {
		// We are running a one-off command locally, so we don't need to worry too much about rate limitting
		// Bumping this up greatly decreases install time
		config.QPS = 50
		config.Burst = 100
	})
	if err != nil {
		return nil, nil, err
	}
	kubeClient, err := kube.NewCLIClient(kube.NewClientConfigForRestConfig(rc), "")
	if err != nil {
		return nil, nil, fmt.Errorf("create Kubernetes client: %v", err)
	}
	client, err := client.New(kubeClient.RESTConfig(), client.Options{Scheme: kube.IstioScheme})
	if err != nil {
		return nil, nil, err
	}
	if err := k8sversion.IsK8VersionSupported(kubeClient, l); err != nil {
		return nil, nil, fmt.Errorf("check minimum supported Kubernetes version: %v", err)
	}
	return kubeClient, client, nil
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

func applyManifest(kubeClient kube.Client, client client.Client, manifestStr string,
	componentName name.ComponentName, opts *applyOptions, iop *v1alpha1.IstioOperator, l clog.Logger,
) error {
	// Needed in case we are running a test through this path that doesn't start a new process.
	cache.FlushObjectCaches()
	reconciler, err := helmreconciler.NewHelmReconciler(client, kubeClient, iop, &helmreconciler.Options{DryRun: opts.DryRun, Log: l})
	if err != nil {
		l.LogAndError(err)
		return err
	}
	ms := name.Manifest{
		Name:    componentName,
		Content: manifestStr,
	}
	_, _, err = reconciler.ApplyManifest(ms, reconciler.CheckSSAEnabled())
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

	b, err := os.ReadFile(filePath)
	if err != nil {
		return "", "", fmt.Errorf("could not read values from file %s: %s", filePath, err)
	}
	customResource = string(b)
	istioNamespace = mergedIOPS.Namespace
	return
}

// createNamespace creates a namespace using the given k8s interface.
func createNamespace(cs kubernetes.Interface, namespace string, network string, dryRun bool) error {
	return helmreconciler.CreateNamespace(cs, namespace, network, dryRun)
}

// saveIOPToCluster saves the state in an IOP CR in the cluster.
func saveIOPToCluster(reconciler *helmreconciler.HelmReconciler, iop string) error {
	obj, err := object.ParseYAMLToK8sObject([]byte(iop))
	if err != nil {
		return err
	}
	return reconciler.ApplyObject(obj.UnstructuredObject(), false)
}
