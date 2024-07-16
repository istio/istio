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

	"sigs.k8s.io/controller-runtime/pkg/client"
	controllruntimelog "sigs.k8s.io/controller-runtime/pkg/log"

	"istio.io/istio/istioctl/pkg/install/k8sversion"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
)

// installerScope is the scope for all commands in the mesh package.
var installerScope = log.RegisterScope("installer", "installer")

func init() {
	// adding to remove message about the controller-runtime logs not getting displayed
	// We cannot do this in the `log` package since it would place a runtime dependency on controller-runtime for all binaries.
	scope := log.RegisterScope("controlleruntime", "scope for controller runtime")
	controllruntimelog.SetLogger(log.NewLogrAdapter(scope))
}

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

func refreshGoldenFiles() bool {
	ev := os.Getenv("REFRESH_GOLDEN")
	return ev == "true" || ev == "1"
}

func kubeBuilderInstalled() bool {
	ev := os.Getenv("KUBEBUILDER")
	return ev == "true" || ev == "1"
}

// Confirm waits for a user to confirm with the supplied message.
func Confirm(msg string, writer io.Writer) bool {
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

func KubernetesClients(kubeClient kube.CLIClient, l clog.Logger) (kube.CLIClient, client.Client, error) {
	client, err := client.New(kubeClient.RESTConfig(), client.Options{Scheme: kube.IstioScheme})
	if err != nil {
		return nil, nil, err
	}
	if err := k8sversion.IsK8VersionSupported(kubeClient, l); err != nil {
		return nil, nil, fmt.Errorf("check minimum supported Kubernetes version: %v", err)
	}
	return kubeClient, client, nil
}

// --manifests is an alias for --set installPackagePath=
// --revision is an alias for --set revision=
func applyFlagAliases(flags []string, manifestsPath, revision string) []string {
	if manifestsPath != "" {
		flags = append(flags, fmt.Sprintf("installPackagePath=%s", manifestsPath))
	}
	if revision != "" && revision != "default" {
		flags = append(flags, fmt.Sprintf("revision=%s", revision))
	}
	return flags
}
