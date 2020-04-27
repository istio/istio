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

// Package mesh contains types and functions that are used across the full
// set of mixer commands.
package mesh

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/releaseutil"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/pkg/log"
)

var (
	// Path to the operator install base dir in the snapshot. This symbol is required here because it's referenced
	// in "operator dump" e2e command tests and there's no other way to inject a path into the snapshot into the command.
	snapshotInstallPackageDir string
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

// Options contains the startup options for applying the manifest.
type Options struct {
	// Path to the kubeconfig file.
	Kubeconfig string
	// ComponentName of the kubeconfig context to use.
	Context string
	// DryRun performs all steps except actually applying the manifests or creating output dirs/files.
	DryRun bool
	// Wait for resources to be ready after install.
	Wait bool
	// Maximum amount of time to wait for resources to be ready after install when Wait=true.
	WaitTimeout time.Duration
}

func applyManifest(restConfig *rest.Config, client client.Client, manifestStr, componentName string, opts *Options, l clog.Logger) error {
	// Needed in case we are running a test through this path that doesn't start a new process.
	helmreconciler.FlushObjectCaches()
	reconciler, err := helmreconciler.NewHelmReconciler(client, restConfig, nil, &helmreconciler.Options{DryRun: opts.DryRun, Log: l})
	if err != nil {
		l.LogAndError(err)
		return err
	}
	ms := []releaseutil.Manifest{{
		Name:    componentName,
		Content: manifestStr,
	}}
	_, _, err = reconciler.ProcessManifest(ms, true)
	return err
}

func getCRAndNamespaceFromFile(filePath string, l clog.Logger) (customResource string, istioNamespace string, err error) {
	if filePath == "" {
		return "", "", nil
	}

	_, mergedIOPS, err := GenerateConfig([]string{filePath}, "", false, nil, l)
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
