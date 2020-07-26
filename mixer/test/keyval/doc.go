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

// nolint
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -t mixer/test/keyval/template.proto
// nolint
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -a mixer/test/keyval/config.proto -x "-s=false -n keyval -t keyval -d example"

// Package keyval contains the sources for a demo route directive adapter.
package keyval
