// Copyright 2018 Istio Authors
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
//go:generate $GOPATH/src/istio.io/istio/mixer/bin/generate_word_list.py $GOPATH/src/istio.io/istio/vendor/istio.io/api/mixer/v1/global_dictionary.yaml list.gen.go

// Package attribute is focused on enabling efficient handling and tracking of
// attribute usage within Mixer.
package attribute
