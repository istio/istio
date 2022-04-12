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
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"

	"istio.io/pkg/log"
)

var (
	// TARGET_OUT environment variable
	// nolint: golint, revive, stylecheck
	TARGET_OUT Variable = "TARGET_OUT"

	// LOCAL_OUT environment variable
	// nolint: golint, revive, stylecheck
	LOCAL_OUT Variable = "LOCAL_OUT"

	// REPO_ROOT environment variable
	// nolint: golint, revive, stylecheck
	REPO_ROOT Variable = "REPO_ROOT"

	// HUB is the Docker hub to be used for images.
	// nolint: golint, revive, stylecheck
	HUB Variable = "HUB"

	// TAG is the Docker tag to be used for images.
	// nolint: golint, revive, stylecheck
	TAG Variable = "TAG"

	// PULL_POLICY is the image pull policy to use when rendering templates.
	// nolint: golint, revive, stylecheck
	PULL_POLICY Variable = "PULL_POLICY"

	// KUBECONFIG is the list of Kubernetes configuration files. If configuration files are specified on
	// the command-line, that takes precedence.
	// nolint: golint, revive, stylecheck
	KUBECONFIG Variable = "KUBECONFIG"

	// IstioSrc is the location of istio source ($TOP/src/istio.io/istio
	IstioSrc = REPO_ROOT.ValueOrDefaultFunc(getDefaultIstioSrc)

	// IstioOut is the location of the output directory ($TOP/out)
	IstioOut = verifyFile(TARGET_OUT, TARGET_OUT.ValueOrDefaultFunc(getDefaultIstioOut))

	// LocalOut is the location of the output directory for the OS we are running in,
	// not necessarily the OS we are building for
	LocalOut = verifyFile(LOCAL_OUT, LOCAL_OUT.ValueOrDefaultFunc(getDefaultIstioOut))

	// OtelCollectorInstallFilePath is the OpenTelemetry installation file.
	OtelCollectorInstallFilePath = path.Join(IstioSrc, getInstallationFile("opentelemetry/opentelemetry-collector.yaml"))

	// StackdriverInstallFilePath is the stackdriver installation file.
	StackdriverInstallFilePath = path.Join(IstioSrc, getInstallationFile("stackdriver/stackdriver.yaml"))

	// GCEMetadataServerInstallFilePath is the GCE Metadata Server installation file.
	GCEMetadataServerInstallFilePath = path.Join(IstioSrc, getInstallationFile("gcemetadata/gce_metadata_server.yaml"))

	// ContainerRegistryServerInstallFilePath is the fake container registry installation file.
	ContainerRegistryServerInstallFilePath = path.Join(IstioSrc, getInstallationFile("containerregistry/container_registry_server.yaml"))
)

var (
	_, b, _, _ = runtime.Caller(0)

	// Root folder of this project
	// This relies on the fact this file is 3 levels up from the root; if this changes, adjust the path below
	Root = filepath.Join(filepath.Dir(b), "../../..")
)

func getDefaultIstioSrc() string {
	return Root
}

func getInstallationFile(p string) string {
	return fmt.Sprintf("pkg/test/framework/components/%s", p)
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

func ReadProxySHA() (string, error) {
	type DepsFile struct {
		Name          string `json:"name"`
		LastStableSHA string `json:"lastStableSHA"`
	}
	f := filepath.Join(IstioSrc, "istio.deps")
	depJSON, err := os.ReadFile(f)
	if err != nil {
		return "", err
	}
	var deps []DepsFile
	if err := json.Unmarshal(depJSON, &deps); err != nil {
		return "", err
	}
	for _, d := range deps {
		if d.Name == "PROXY_REPO_SHA" {
			return d.LastStableSHA, nil
		}
	}
	return "", fmt.Errorf("PROXY_REPO_SHA not found")
}
