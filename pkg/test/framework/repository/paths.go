//  Copyright 2018 Istio Authors
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

// Package repository have utility methods related to accessing a local istio code repository. The repository
// path is deduced from ${GOPATH} environment variable.
package repository

import (
	"os"
	"path"
)

// Root of the repository
func Root() string {
	return path.Join(os.Getenv("GOPATH"), "src/istio.io/istio")
}

// ChartsDir is the Kubernetes Helm chart directory in the repository
func ChartsDir() string {
	return path.Join(Root(), "install/kubernetes/helm")
}

// IstioChartDir is Istio Helm chart directory
func IstioChartDir() string {
	return path.Join(ChartsDir(), "istio")
}
