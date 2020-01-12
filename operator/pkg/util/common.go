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

package util

import (
	"net/url"
	"strings"

	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/pkg/log"
)

var (
	scope = log.RegisterScope("util", "util", 0)
	// IstioOperatorGVK is GVK for IstioOperator
	IstioOperatorGVK = schema.GroupVersionKind{
		Version: "v1alpha1",
		Group:   "install.istio.io",
		Kind:    "IstioOperator",
	}
)

// Tree is a tree.
type Tree map[string]interface{}

// String implements the Stringer interface method.
func (t Tree) String() string {
	y, err := yaml.Marshal(t)
	if err != nil {
		return err.Error()
	}
	return string(y)
}

// IsFilePath reports whether the given URL is a local file path.
func IsFilePath(path string) bool {
	return strings.Contains(path, "/") || strings.Contains(path, ".")
}

// IsHTTPURL checks whether the given URL is a HTTP URL, empty path or relative URLs would be rejected.
func IsHTTPURL(path string) bool {
	u, err := url.Parse(path)
	return err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https")
}
