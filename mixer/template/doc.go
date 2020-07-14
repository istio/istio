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

// apikey
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -t mixer/template/apikey/template.proto

// authorization
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -t mixer/template/authorization/template.proto

// checknothing
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -t mixer/template/checknothing/template.proto

// edge
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -t mixer/template/edge/template.proto

// listentry
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -t mixer/template/listentry/template.proto

// logentry
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -t mixer/template/logentry/template.proto

// metric
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -t mixer/template/metric/template.proto

// quota
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -t mixer/template/quota/template.proto

// reportnothing
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -t mixer/template/reportnothing/template.proto

// tracespan
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -t mixer/template/tracespan/tracespan.proto

// template.gen.go
// nolint
//go:generate go run $REPO_ROOT/mixer/tools/codegen/cmd/mixgenbootstrap/main.go -f $REPO_ROOT/mixer/template/inventory.yaml -o $REPO_ROOT/mixer/template/template.gen.go

// Package template provides runtime descriptors of the templates known
// to Mixer at compile-time.
package template
