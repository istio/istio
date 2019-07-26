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
	pkgversion "istio.io/operator/pkg/version"
)

const (
	// OperatorBinaryVersionString is the Istio operator version.
	OperatorBinaryVersionString = "1.3.0"
)

var (
	// SupportedVersions is a list of chart versions supported by this version of the operator.
	// It must be synced with the versions.yaml file.
	SupportedVersions = []string{
		"1.3.0",
	}

	// OperatorBinaryVersion is the Istio operator version.
	OperatorBinaryVersion pkgversion.Version
)

func init() {
	v, err := pkgversion.NewVersionFromString(OperatorBinaryVersionString)
	if err != nil {
		panic(err)
	}
	OperatorBinaryVersion = *v
}
