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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/pkg/log"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/cache"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/kube"
)

var (
	// installerScope is the scope for all commands in the mesh package.
	installerScope = log.RegisterScope("installer", "installer", 0)
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
	return os.Getenv("REFRESH_GOLDEN") == "true"
}

func ReadLayeredYAMLs(filenames []string) (string, error) {
	return readLayeredYAMLs(filenames, os.Stdin)
}

func readLayeredYAMLs(filenames []string, stdinReader io.Reader) (string, error) {
	var ly string
	var stdin bool
	for _, fn := range filenames {
		var b []byte
		var err error
		if fn == "-" {
			if stdin {
				continue
			}
			stdin = true
			b, err = ioutil.ReadAll(stdinReader)
		} else {
			b, err = ioutil.ReadFile(strings.TrimSpace(fn))
		}
		if err != nil {
			return "", err
		}
		ly, err = util.OverlayYAML(ly, string(b))
		if err != nil {
			return "", err
		}
	}
	return ly, nil
}

// confirm waits for a user to confirm with the supplied message.
func confirm(msg string, writer io.Writer) bool {
	_, _ = fmt.Fprintf(writer, "%s ", msg)

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

func k8sClient(kubeConfigPath string, context string) (kube.Client, error) {
	restCfg, err := kube.DefaultRestConfig(kubeConfigPath, context, func(config *rest.Config) {
		config.QPS = 50
		config.Burst = 100
	})
	if err != nil {
		return nil, err
	}
	return kube.NewClient(kube.NewClientConfigForRestConfig(restCfg))
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
	componentName name.ComponentName, opts *applyOptions, l clog.Logger) error {
	// Needed in case we are running a test through this path that doesn't start a new process.
	cache.FlushObjectCaches()
	reconciler, err := helmreconciler.NewHelmReconciler(client, restConfig, nil, &helmreconciler.Options{DryRun: opts.DryRun, Log: l})
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

// getCRAndNamespaceFromFile returns the CR name and istio namespace from a file containing an IstioOperator CR.
func getCRAndNamespaceFromFile(filePath string, l clog.Logger) (customResource string, istioNamespace string, err error) {
	if filePath == "" {
		return "", "", nil
	}

	_, mergedIOPS, err := GenerateConfig(GenerateConfigOptions{
		InFilenames: []string{filePath},
	}, l)
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

// deleteNamespace deletes namespace using the given k8s client.
func deleteNamespace(cs kubernetes.Interface, namespace string) error {
	return cs.CoreV1().Namespaces().Delete(context.TODO(), namespace, v12.DeleteOptions{})
}
