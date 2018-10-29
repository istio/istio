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
// HelmDepUpdate helm dep update to update dependencies for umrella charts
func HelmDepUpdate(chartDir string) error {
	_, err := Shell("helm dep update %s", chartDir)
	return err
}

// HelmInstallDryRun helm install dry run from a chart for a given namespace
func HelmInstallDryRun(chartDir, chartName, namespace, setValue string) error {
	_, err := Shell("helm install --dry-run --debug %s --name %s --namespace %s %s", chartDir, chartName, namespace, setValue)
	return err
}

// HelmInstall helm install from a chart for a given namespace
//       --set stringArray        set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)
func HelmInstall(chartDir, chartName, namespace, setValue string) error {
	_, err := Shell("helm install %s --name %s --namespace %s %s", chartDir, chartName, namespace, setValue)
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
