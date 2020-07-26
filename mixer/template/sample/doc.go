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

// Codegen blocks

// apa
//go:generate $REPO_ROOT/bin/mixer_codegen.sh  -d false -t mixer/template/sample/apa/Apa.proto

// check
//go:generate $REPO_ROOT/bin/mixer_codegen.sh  -d false -t mixer/template/sample/check/CheckTesterTemplate.proto

// quota
//go:generate $REPO_ROOT/bin/mixer_codegen.sh  -d false -t mixer/template/sample/quota/QuotaTesterTemplate.proto

// report
//go:generate $REPO_ROOT/bin/mixer_codegen.sh  -d false -t mixer/template/sample/report/ReportTesterTemplate.proto

// template.gen.go
// nolint
//go:generate go run $REPO_ROOT/mixer/tools/codegen/cmd/mixgenbootstrap/main.go -f $REPO_ROOT/mixer/template/sample/inventory.yaml -o $REPO_ROOT/mixer/template/sample/template.gen.go

// Package sample provides a set of templates for internal testing of Mixer.
// Templates under this directory are for Mixer's internal testing purpose *ONLY*.
package sample
