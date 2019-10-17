// Copyright 2019 Istio Authors
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
	goversion "github.com/hashicorp/go-version"

	pkgversion "istio.io/operator/pkg/version"
)

const (
	// OperatorVersionString is the version string of this operator binary.
	OperatorVersionString = "1.4.0"
)

var (
	// OperatorBinaryVersion is the Istio operator version.
	OperatorBinaryVersion pkgversion.Version
	// OperatorBinaryGoVersion is the Istio operator version in go-version format.
	OperatorBinaryGoVersion *goversion.Version
)

func init() {
	var err error
	OperatorBinaryGoVersion, err = goversion.NewVersion(OperatorVersionString)
	if err != nil {
		panic(err)
	}
	v, err := pkgversion.NewVersionFromString(OperatorVersionString)
	if err != nil {
		panic(err)
	}
	OperatorBinaryVersion = *v
}
