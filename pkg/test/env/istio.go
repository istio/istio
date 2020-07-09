//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package env

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"runtime"

	"istio.io/pkg/log"
)

var (
	// ISTIO_OUT environment variable
	// nolint: golint, stylecheck
	ISTIO_OUT Variable = "ISTIO_OUT"

	// LOCAL_OUT environment variable
	// nolint: golint, stylecheck
	LOCAL_OUT Variable = "LOCAL_OUT"

	// REPO_ROOT environment variable
	// nolint: golint, stylecheck
	REPO_ROOT Variable = "REPO_ROOT"

	// HUB is the Docker hub to be used for images.
	// nolint: golint, stylecheck
	HUB Variable = "HUB"

	// TAG is the Docker tag to be used for images.
	// nolint: golint, stylecheck
	TAG Variable = "TAG"

	// BITNAMIHUB is the Docker registry to be used for the bitnami images.
	// nolint: golint
	BITNAMIHUB Variable = "BITNAMIHUB"

	// PULL_POLICY is the image pull policy to use when rendering templates.
	// nolint: golint, stylecheck
	PULL_POLICY Variable = "PULL_POLICY"

	// ISTIO_TEST_KUBE_CONFIG is the Kubernetes configuration file to use for testing. If a configuration file
	// is specified on the command-line, that takes precedence.
	// nolint: golint, stylecheck
	ISTIO_TEST_KUBE_CONFIG Variable = "ISTIO_TEST_KUBE_CONFIG"

	// IstioSrc is the location of istio source ($TOP/src/istio.io/istio
	IstioSrc = REPO_ROOT.ValueOrDefaultFunc(getDefaultIstioSrc)

	// IstioOut is the location of the output directory ($TOP/out)
	IstioOut = verifyFile(ISTIO_OUT, ISTIO_OUT.ValueOrDefaultFunc(getDefaultIstioOut))

	// LocalOut is the location of the output directory for the OS we are running in,
	// not necessarily the OS we are building for
	LocalOut = verifyFile(LOCAL_OUT, LOCAL_OUT.ValueOrDefaultFunc(getDefaultIstioOut))

	// TODO: Some of these values are overlapping. We should re-align them.

	// ChartsDir is the Kubernetes Helm chart directory in the repository
	ChartsDir = path.Join(IstioSrc, "install/kubernetes/helm")

	// BookInfoRoot is the root folder for the bookinfo samples
	BookInfoRoot = path.Join(IstioSrc, "samples/bookinfo")

	// BookInfoKube is the book info folder that contains Yaml deployment files.
	BookInfoKube = path.Join(BookInfoRoot, "platform/kube")

	// ServiceAccountFilePath is the helm service account file.
	ServiceAccountFilePath = path.Join(IstioSrc, "pkg/test/framework/components/redis/service_account.yaml")

	// RedisInstallFilePath is the redis installation file.
	RedisInstallFilePath = path.Join(IstioSrc, "pkg/test/framework/components/redis/redis.yaml")

	// StackdriverInstallFilePath is the stackdriver installation file.
	StackdriverInstallFilePath = path.Join(IstioSrc, "pkg/test/framework/components/stackdriver/stackdriver.yaml")

	// GCEMetadataServerInstallFilePath is the GCE Metadata Server installation file.
	GCEMetadataServerInstallFilePath = path.Join(IstioSrc, "pkg/test/framework/components/gcemetadata/gce_metadata_server.yaml")
)

func getDefaultIstioSrc() string {
	// Assume it is run inside istio.io/istio
	current, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	idx := strings.Index(current, filepath.Join("/src", "istio.io", "istio"))
	if idx > 0 {
		return filepath.Join(current[0:idx], "/src", "istio.io", "istio")
	}
	return current // launching from GOTOP (for example in goland)
}

func getDefaultIstioOut() string {
	return fmt.Sprintf("%s/out/%s_%s", IstioSrc, runtime.GOOS, runtime.GOARCH)
}

func verifyFile(v Variable, f string) string {
	if !fileExists(f) {
		log.Warnf("unable to resolve %s. Dir %s does not exist", v, f)
		return ""
	}
	return f
}

func fileExists(f string) bool {
	return CheckFileExists(f) == nil
}

func CheckFileExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return err
	}
	return nil
}
