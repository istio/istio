/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rules

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/lint/support"
)

// Values lints a chart's values.yaml file.
func Values(linter *support.Linter) {
	file := "values.yaml"
	vf := filepath.Join(linter.ChartDir, file)
	fileExists := linter.RunLinterRule(support.InfoSev, file, validateValuesFileExistence(linter, vf))

	if !fileExists {
		return
	}

	linter.RunLinterRule(support.ErrorSev, file, validateValuesFile(linter, vf))
}

func validateValuesFileExistence(linter *support.Linter, valuesPath string) error {
	_, err := os.Stat(valuesPath)
	if err != nil {
		return fmt.Errorf("file does not exist")
	}
	return nil
}

func validateValuesFile(linter *support.Linter, valuesPath string) error {
	_, err := chartutil.ReadValuesFile(valuesPath)
	if err != nil {
		return fmt.Errorf("unable to parse YAML\n\t%s", err)
	}
	return nil
}
