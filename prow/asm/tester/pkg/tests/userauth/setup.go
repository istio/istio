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

package userauth

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"istio.io/istio/prow/asm/tester/pkg/exec"
	"istio.io/istio/prow/asm/tester/pkg/resource"
)

func Setup(settings *resource.Settings) error {
	if err := installASMUserAuth(settings); err != nil {
		return fmt.Errorf("error installing user auth: %w", err)
	}
	if err := downloadUserAuthDependencies(settings); err != nil {
		return fmt.Errorf("error installing user auth dependencies: %w", err)
	}

	// TODO(b/182912549): port-forward in go test code instead of here
	go exec.Run("kubectl port-forward service/istio-ingressgateway 8443:443 -n istio-system")

	return nil
}

func installASMUserAuth(settings *resource.Settings) error {
	cmds := []string{
		"kubectl create namespace asm-user-auth",
		"kubectl label namespace asm-user-auth istio-injection=enabled --overwrite",

		"kubectl create namespace userauth-test",
		"kubectl label namespace userauth-test istio-injection=enabled --overwrite",

		// TODO(b/182914654): deploy app in go code
		"kubectl -n userauth-test apply -f https://raw.githubusercontent.com/istio/istio/master/samples/httpbin/httpbin.yaml",
		"kubectl wait --for=condition=Ready --timeout=2m --namespace=userauth-test --all pod",

		// Create the kubernetes secret for the encryption and signing key.
		fmt.Sprintf(`kubectl create secret generic secret-key  \
			--from-file="session_cookie.key"="%s/user-auth/aes_symmetric_key.json"  \
			--from-file="rctoken.key"="%s/user-auth/rsa_signing_key.json"  \
			--namespace=asm-user-auth`, settings.ConfigDir, settings.ConfigDir),

		fmt.Sprintf("kpt pkg get https://github.com/GoogleCloudPlatform/asm-user-auth.git/pkg@main %s/user-auth/pkg", settings.ConfigDir),
		fmt.Sprintf("kubectl apply -f %s/user-auth/pkg/asm_user_auth_config_v1alpha1.yaml", settings.ConfigDir),
	}
	if err := exec.RunMultiple(cmds); err != nil {
		return err
	}

	var res map[string]interface{}
	// TODO(b/182940034): use ASM owned account once created
	odicKeys, err := ioutil.ReadFile(fmt.Sprintf("%s/user-auth/userauth_oidc.json", settings.ConfigDir))
	if err != nil {
		return fmt.Errorf("error reading the odic key file: %w", err)
	}
	json.Unmarshal(odicKeys, &res)
	oidcClientID := res["client_id"].(string)
	oidcClientSecret := res["client_secret"].(string)
	oidcIssueURI := res["issuer"].(string)
	// TODO(b/182918059): Fetch image from GCR release repo and GitHub packet.
	userAuthImage := "gcr.io/gke-release-staging/asm/asm_user_auth:staging"
	cmds = []string{
		fmt.Sprintf("kpt cfg set %s/user-auth/pkg anthos.servicemesh.user-auth.image %s", settings.ConfigDir, userAuthImage),
		fmt.Sprintf("kpt cfg set %s/user-auth/pkg anthos.servicemesh.user-auth.oidc.clientID %s", settings.ConfigDir, oidcClientID),
		fmt.Sprintf("kpt cfg set %s/user-auth/pkg anthos.servicemesh.user-auth.oidc.clientSecret %s", settings.ConfigDir, oidcClientSecret),
		fmt.Sprintf("kpt cfg set %s/user-auth/pkg anthos.servicemesh.user-auth.oidc.issuerURI %s", settings.ConfigDir, oidcIssueURI),
		fmt.Sprintf("kubectl apply -R -f %s/user-auth/pkg/", settings.ConfigDir),
		"kubectl wait --for=condition=Ready --timeout=2m --namespace=asm-user-auth --all pod",
		fmt.Sprintf("kubectl apply -f %s/user-auth/httpbin-route.yaml", settings.ConfigDir),
	}
	if err := exec.RunMultiple(cmds); err != nil {
		return err
	}

	return nil
}

func downloadUserAuthDependencies(settings *resource.Settings) error {
	// need this mkdir for installing jre: https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=863199
	if err := os.MkdirAll("/usr/share/man/man1", 0755); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(settings.ConfigDir, "user-auth/dependencies"), 0755); err != nil {
		return err
	}

	latestChangeURL := "https://www.googleapis.com/download/storage/v1/b/chromium-browser-snapshots/o/Linux_x64%2FLAST_CHANGE?alt=media"
	revision, err := exec.RunWithOutput("curl -s -S " + latestChangeURL)
	if err != nil {
		return err
	}
	chromiumURL := "https://www.googleapis.com/download/storage/v1/b/chromium-browser-snapshots/o/Linux_x64%2F" + revision + "%2Fchrome-linux.zip?alt=media"
	driverURL := "https://www.googleapis.com/download/storage/v1/b/chromium-browser-snapshots/o/Linux_x64%2F" + revision + "%2Fchromedriver_linux64.zip?alt=media"
	seleniumURL := "https://selenium-release.storage.googleapis.com/3.141/selenium-server-standalone-3.141.59.jar"
	cmds := []string{
		//  TODO(b/182939536): add apt-get to https://github.com/istio/tools/blob/master/docker/build-tools/Dockerfile
		"bash -c 'apt-get update && apt-get install -y --no-install-recommends unzip openjdk-11-jre xvfb chromium-browser'",

		fmt.Sprintf("bash -c 'curl -# %s > %s/user-auth/dependencies/chrome-linux.zip'", chromiumURL, settings.ConfigDir),
		fmt.Sprintf("bash -c 'curl -# %s > %s/user-auth/dependencies/chromedriver-linux.zip'", driverURL, settings.ConfigDir),
		fmt.Sprintf("unzip %s/user-auth/dependencies/chrome-linux.zip -d %s/user-auth/dependencies", settings.ConfigDir, settings.ConfigDir),
		fmt.Sprintf("unzip %s/user-auth/dependencies/chromedriver-linux.zip -d %s/user-auth/dependencies", settings.ConfigDir, settings.ConfigDir),
		fmt.Sprintf("bash -c 'curl -# %s > %s/user-auth/dependencies/selenium-server.jar'", seleniumURL, settings.ConfigDir),
	}
	if err = exec.RunMultiple(cmds); err != nil {
		return err
	}

	// need it for DevToolsActivePorts error, https://yaqs.corp.google.com/eng/q/5322136407900160
	return os.Setenv("DISPLAY", ":20")
}
