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
	"errors"
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
	// ISTIO_GO environment variable
	// nolint: golint
	ISTIO_GO Variable = "ISTIO_GO"

	// ISTIO_BIN environment variable
	// nolint: golint
	ISTIO_BIN Variable = "ISTIO_BIN"

	// ISTIO_OUT environment variable
	// nolint: golint
	ISTIO_OUT Variable = "ISTIO_OUT"

	// HUB is the Docker hub to be used for images.
	// nolint: golint
	HUB Variable = "HUB"

	// TAG is the Docker tag to be used for images.
	// nolint: golint
	TAG Variable = "TAG"

	// PULL_POLICY is the image pull policy to use when rendering templates.
	// nolint: golint
	PULL_POLICY Variable = "PULL_POLICY"

	// ISTIO_TEST_KUBE_CONFIG is the Kubernetes configuration file to use for testing. If a configuration file
	// is specified on the command-line, that takes precedence.
	// nolint: golint
	ISTIO_TEST_KUBE_CONFIG Variable = "ISTIO_TEST_KUBE_CONFIG"

	// IstioSrc is the location if istio source ($TOP/src/istio.io/istio).
	IstioSrc = getIstioSrc()

	// IstioBin is the location of the binary output directory
	IstioBin = artifactDir(ISTIO_BIN, "bin")

	// IstioOut is the location of the output directory ($TOP/out)
	IstioOut = artifactDir(ISTIO_OUT, "out", fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH))

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

func moduleRoot(dir string) (string, error) {
	for {
		switch _, err := os.Stat(filepath.Join(dir, "go.mod")); {
		case os.IsNotExist(err):
			parent := filepath.Dir(dir)
			if parent == dir {
				// TODO: Find a better message, this may be confusing if not using Go modules.
				return "", errors.New("not a Go module")
			}
			dir = parent
		case err != nil:
			return "", err
		default:
			return dir, nil
		}
	}
}

func insideGoPath(dir string) bool {
	pref := build.Default.GOPATH
	if !strings.HasPrefix(dir, pref) {
		return false
	}
	return len(dir) == len(pref) || dir[len(pref)] == filepath.Separator
}

func getIstioSrc() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	// Even if we're inside GOPATH and not respect modules, moduleRoot is a good way to find the
	// repository root no matter the name (which, in GOPATH, should be istio.io/istio).
	dir, err = moduleRoot(dir)
	if err != nil {
		panic(err)
	}
	return dir
}

func verifyFile(v Variable, f string) string {
	if err := CheckFileExists(f); err != nil {
		// The error should contain the file path.
		log.Fatalf("Unable to resolve %s: %v", v, err)
	}
	return f
}

func CheckFileExists(path string) error {
	_, err := os.Stat(path)
	return err
}

func artifactDir(v Variable, kind string, subdirs ...string) string {
	if x := v.Value(); x != "" {
		return verifyFile(v, x)
	}
	root := IstioSrc
	if insideGoPath(IstioSrc) {
		root = build.Default.GOPATH
	} else {
		kind = "istio_" + kind
	}
	dirs := append([]string{root, kind}, subdirs...)
	return verifyFile(v, filepath.Join(dirs...))
}
