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

// template.gen.go
// nolint
//go:generate go run $REPO_ROOT/mixer/tools/codegen/cmd/mixgenbootstrap/main.go -f $REPO_ROOT/mixer/test/spyAdapter/template/inventory.yaml -o $REPO_ROOT/mixer/test/spyAdapter/template/template.gen.go

// Package template contains generated code for the spy adapter testing. It
// should *ONLY* be used for testing Mixer.
package template
