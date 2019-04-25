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

package lint // import "k8s.io/helm/pkg/lint"

import (
	"path/filepath"

	"k8s.io/helm/pkg/lint/rules"
	"k8s.io/helm/pkg/lint/support"
)

// All runs all of the available linters on the given base directory.
func All(basedir string, values []byte, namespace string, strict bool) support.Linter {
	// Using abs path to get directory context
	chartDir, _ := filepath.Abs(basedir)

	linter := support.Linter{ChartDir: chartDir}
	rules.Chartfile(&linter)
	rules.Values(&linter)
	rules.Templates(&linter, values, namespace, strict)
	return linter
}
