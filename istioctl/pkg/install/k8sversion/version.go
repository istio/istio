// Copyright Istio Authors.
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

package k8sversion

import (
	"fmt"

	goversion "github.com/hashicorp/go-version"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"istio.io/istio/operator/pkg/util/clog"
	pkgVersion "istio.io/pkg/version"
)

const (
	// MinK8SVersion is the minimum k8s version required to run this version of Istio
	// https://istio.io/docs/setup/platform-setup/
	MinK8SVersion = 17
)

// CheckKubernetesVersion checks if this Istio version is supported in the k8s version
func CheckKubernetesVersion(versionInfo *version.Info) (bool, error) {
	v, err := extractKubernetesVersion(versionInfo)
	if err != nil {
		return false, err
	}
	return MinK8SVersion <= v, nil
}

// extractKubernetesVersion returns the Kubernetes minor version. For example, `v1.19.1` will return `19`
func extractKubernetesVersion(versionInfo *version.Info) (int, error) {
	ver, err := goversion.NewVersion(versionInfo.String())
	if err != nil {
		return 0, fmt.Errorf("could not parse %v", err)
	}
	// Segments provide slice of int eg: v1.19.1 => [1, 19, 1]
	num := ver.Segments()[1]
	return num, nil
}

// GetKubernetesVersion fetches the Kubernetes minor version. For example, `v1.19.1` will return `19`
func GetKubernetesVersion(restConfig *rest.Config) (int, error) {
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return 0, fmt.Errorf("error creating Kubernetes client: %w", err)
	}
	serverVersion, err := client.Discovery().ServerVersion()
	if err != nil {
		return 0, fmt.Errorf("error getting Kubernetes version: %w", err)
	}
	return extractKubernetesVersion(serverVersion)
}

// IsK8VersionSupported checks minimum supported Kubernetes version for istio
func IsK8VersionSupported(clientset kubernetes.Interface, l clog.Logger) error {
	serverVersion, err := clientset.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("error getting Kubernetes version: %w", err)
	}
	ok, err := CheckKubernetesVersion(serverVersion)
	if err != nil {
		return fmt.Errorf("error checking if Kubernetes version is supported: %w", err)
	}
	if !ok {
		l.LogAndPrintf("\nThe Kubernetes version %s is not supported by Istio %s. The minimum supported Kubernetes version is 1.%d.\n"+
			"Proceeding with the installation, but you might experience problems. "+
			"See https://istio.io/latest/docs/setup/platform-setup/ for a list of supported versions.\n",
			serverVersion.GitVersion, pkgVersion.Info.Version, MinK8SVersion)
	}
	return nil
}
