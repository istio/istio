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

package data

var (
	// YamlN1I1V1 is a testing resource in Yaml form
	YamlN1I1V1 = `
apiVersion: testdata.istio.io/v1alpha1
kind: Kind1
metadata:
  namespace: n1
  name: i1
spec:
  n1_i1: v1
`

	// YamlN1I1V2 is a testing resource in Yaml form
	YamlN1I1V2 = `
apiVersion: testdata.istio.io/v1alpha1
kind: Kind1
metadata:
  namespace: n1
  name: i1
spec:
  n1_i1: v2
`

	// YamlN2I2V1 is a testing resource in Yaml form
	YamlN2I2V1 = `
apiVersion: testdata.istio.io/v1alpha1
kind: Kind1
metadata:
  namespace: n2
  name: i2
spec:
  n2_i2: v1
`
	// YamlN2I2V2 is a testing resource in Yaml form
	YamlN2I2V2 = `
apiVersion: testdata.istio.io/v1alpha1
kind: Kind1
metadata:
  namespace: n2
  name: i2
spec:
  n2_i2: v2
`

	// YamlN3I3V1 is a testing resource in Yaml form
	YamlN3I3V1 = `
apiVersion: testdata.istio.io/v1alpha1
kind: Kind1
metadata:
  namespace: n3
  name: i3
spec:
  n3_i3: v1
`

	// YamlUnrecognized is a testing resource in Yaml form
	YamlUnrecognized = `
apiVersion: testdata.istio.io/v1alpha1
kind: KindUnknown
metadata:
  namespace: n1
  name: i1
spec:
  n1_i1: v1
`

	// YamlUnparseableResource is a testing resource in Yaml form
	YamlUnparseableResource = `
apiVersion: testdata.istio.io/v1alpha1/foo/bar
kind: Kind1
metadata:
  namespace: n1
  name: i1
spec:
  foo: bar
`

	// YamlNonStringKey is a testing resource in Yaml form
	YamlNonStringKey = `
23: true
`

	// YamlN1I1V1Kind2 is a testing resource in Yaml form
	YamlN1I1V1Kind2 = `
apiVersion: testdata.istio.io/v1alpha1
kind: Kind2
metadata:
  namespace: n1
  name: i1
spec:
  n1_i1: v1
`

	// YamlI1V1NoNamespace is a testing resource in Yaml form
	YamlI1V1NoNamespace = `
apiVersion: testdata.istio.io/v1alpha1
kind: Kind1
metadata:
  name: i1
spec:
  n1_i1: v1
`

	// YamlI1V1NoNamespaceKind2 is a testing resource in Yaml form
	YamlI1V1NoNamespaceKind2 = `
apiVersion: testdata.istio.io/v1alpha1
kind: Kind2
metadata:
  name: i1
spec:
  n1_i1: v1
`

	// YamlI1V1WithCommentContainingDocumentSeparator is a testing resource in
	// yaml form with a comment containing a document separator.
	YamlI1V1WithCommentContainingDocumentSeparator = `
# ---
apiVersion: testdata.istio.io/v1alpha1
kind: Kind1
metadata:
  namespace: n1
  name: i1
spec:
  n1_i1: v1
`
)
