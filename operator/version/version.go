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

package version

import (
	buildversion "istio.io/pkg/version"

	pkgversion "istio.io/istio/operator/pkg/version"
)

const (
	// OperatorCodeBaseVersion is the version string from the code base.
	OperatorCodeBaseVersion = "1.7.0"
)

var (
	// OperatorVersionString is the version string of this operator binary.
	OperatorVersionString string
	// OperatorBinaryVersion is the Istio operator version.
	OperatorBinaryVersion pkgversion.Version
)

func init() {
	var err error
	OperatorVersionString = OperatorCodeBaseVersion
	// If dockerinfo has a tag (e.g., specified by LDFlags), we will use it as the version of operator
	tag := buildversion.DockerInfo.Tag
	if pkgversion.IsVersionString(tag) {
		OperatorVersionString = pkgversion.TagToVersionStringGrace(tag)
	}
	v, err := pkgversion.NewVersionFromString(OperatorVersionString)
	if err != nil {
		panic(err)
	}
	OperatorBinaryVersion = *v
}
