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

// nolint: golint
package fuzz

import (
	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/grpcgen"
)

func FuzzGrpcGenGenerate(data []byte) int {
	f := fuzz.NewConsumer(data)

	proxy := &model.Proxy{}
	err := f.GenerateStruct(proxy)
	if err != nil {
		return 0
	}

	w := &model.WatchedResource{}
	err = f.GenerateStruct(w)
	if err != nil {
		return 0
	}

	updates := &model.PushRequest{}
	err = f.GenerateStruct(updates)
	if err != nil {
		return 0
	}

	generator := &grpcgen.GrpcConfigGenerator{}
	_, _, _ = generator.Generate(proxy, w, updates)

	return 1
}
