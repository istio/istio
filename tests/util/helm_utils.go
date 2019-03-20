// Copyright 2018 Istio Authors
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

package util

import (
	"context"
	"time"

	"istio.io/istio/pkg/log"
)

// HelmInit init helm with a service account
func HelmInit(serviceAccount string) error {
	_, err := Shell("helm init --upgrade --service-account %s", serviceAccount)
	return err
}

// HelmClientInit initializes the Helm client only
func HelmClientInit() error {
	_, err := Shell("helm init --client-only")
	return err
}

// HelmInstallDryRun helm install dry run from a chart for a given namespace
func HelmInstallDryRun(chartDir, chartName, valueFile, namespace, setValue string) error {
	_, err := Shell("helm install --dry-run --debug " + HelmParams(chartDir, chartName, valueFile, namespace, setValue))
	return err
}

// HelmInstall helm install from a chart for a given namespace
//       --set stringArray        set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)
func HelmInstall(chartDir, chartName, valueFile, namespace, setValue string) error {
	_, err := Shell("helm install " + HelmParams(chartDir, chartName, valueFile, namespace, setValue))
	return err
}

// HelmTest helm test a chart release
func HelmTest(releaseName string) error {
	_, err := Shell("helm test %s", releaseName)
	return err
}

// HelmTemplate helm template from a chart for a given namespace
//      --set stringArray        set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)
func HelmTemplate(chartDir, chartName, namespace, setValue, outfile string) error {
	_, err := Shell("helm template %s --name %s --namespace %s %s > %s", chartDir,
		chartName, namespace, setValue, outfile)
	return err
}

// HelmDelete helm del --purge a chart
func HelmDelete(chartName string) error {
	_, err := Shell("helm del --purge %s", chartName)
	return err
}

// HelmParams provides a way to construct helm params
func HelmParams(chartDir, chartName, valueFile, namespace, setValue string) string {
	helmCmd := chartDir + " --name " + chartName + " --namespace " + namespace + " " + setValue
	if valueFile != "" {
		helmCmd = chartDir + " --name " + chartName + " --values " + valueFile + " --namespace " + namespace + " " + setValue
	}

	return helmCmd
}

// Obtain the version of Helm client and server with a timeout of 10s or return an error
func helmVersion() (string, error) {
	version, err := Shell("helm version")
	return version, err
}

// HelmTillerRunning will block for up to 120 seconds waiting for Tiller readiness
func HelmTillerRunning() error {
	retry := Retrier{
		BaseDelay: 10 * time.Second,
		MaxDelay:  10 * time.Second,
		Retries:   12,
	}

	retryFn := func(_ context.Context, i int) error {
		_, err := helmVersion()
		return err
	}
	ctx := context.Background()
	_, err := retry.Retry(ctx, retryFn)
	if err != nil {
		log.Errorf("Tiller failed to start")
		return err
	}
	log.Infof("Tiller is running")
	return nil
}
