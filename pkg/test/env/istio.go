//  Copyright 2018 Istio Authors
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
	"go/build"
	"os"
	"path"
	"path/filepath"
	"strings"

	"runtime"

	"istio.io/pkg/log"
)

var (
	// GOPATH environment variable
	// nolint: golint, stylecheck
	GOPATH Variable = "GOPATH"

	// TOP environment variable
	// nolint: golint, stylecheck
	TOP Variable = "TOP"

	// ISTIO_GO environment variable
	// nolint: golint, stylecheck
	ISTIO_GO Variable = "ISTIO_GO"

	// ISTIO_BIN environment variable
	// nolint: golint, stylecheck
	ISTIO_BIN Variable = "ISTIO_BIN"

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

	// IstioBin is the location of the binary output directory
	IstioBin = verifyFile(ISTIO_BIN, ISTIO_BIN.ValueOrDefaultFunc(getDefaultIstioBin))

	// IstioOut is the location of the output directory ($TOP/out)
	IstioOut = verifyFile(ISTIO_OUT, ISTIO_OUT.ValueOrDefaultFunc(getDefaultIstioOut))

	// LocalOut is the location of the output directory for the OS we are running in,
	// not necessarily the OS we are building for
	LocalOut = verifyFile(LOCAL_OUT, LOCAL_OUT.ValueOrDefaultFunc(getDefaultIstioOut))

	// TODO: Some of these values are overlapping. We should re-align them.

	// ChartsDir is the Kubernetes Helm chart directory in the repository
	ChartsDir = path.Join(IstioSrc, "install/kubernetes/helm")

	// IstioChartDir is the Kubernetes Helm chart directory in the repository
	IstioChartDir = path.Join(ChartsDir, "istio")

	CrdsFilesDir = path.Join(ChartsDir, "istio-init/files")

	// BookInfoRoot is the root folder for the bookinfo samples
	BookInfoRoot = path.Join(IstioSrc, "samples/bookinfo")

	// BookInfoKube is the book info folder that contains Yaml deployment files.
	BookInfoKube = path.Join(BookInfoRoot, "platform/kube")

	// ServiceAccountFilePath is the helm service account file.
	ServiceAccountFilePath = path.Join(ChartsDir, "helm-service-account.yaml")

	// RedisInstallFilePath is the redis installation file.
	RedisInstallFilePath = path.Join(IstioSrc, "pkg/test/framework/components/redis/redis.yaml")
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

func getDefaultIstioBin() string {
	return fmt.Sprintf("%s/bin", build.Default.GOPATH)
}

func getDefaultIstioOut() string {
	return fmt.Sprintf("%s/out/%s_%s", build.Default.GOPATH, runtime.GOOS, runtime.GOARCH)
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
