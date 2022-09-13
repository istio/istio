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

package manifests

import (
	"embed"
	"io/fs"
	"os"
)

// FS embeds the manifests
//
//go:embed charts/* profiles/*
//go:embed charts/gateways/istio-egress/templates/_affinity.tpl
//go:embed charts/gateways/istio-ingress/templates/_affinity.tpl
var FS embed.FS

// BuiltinOrDir returns a FS for the provided directory. If no directory is passed, the compiled in
// FS will be used
func BuiltinOrDir(dir string) fs.FS {
	if dir == "" {
		return FS
	}
	return os.DirFS(dir)
}
