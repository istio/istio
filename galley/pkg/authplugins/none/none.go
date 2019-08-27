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

// Package none is a Galley auth plugin that returns an empty auth
// DialOption.
package none

import (
	"google.golang.org/grpc"

	"istio.io/istio/galley/pkg/authplugin"
)

func returnAuth(_ map[string]string) ([]grpc.DialOption, error) { // nolint: unparam
	return []grpc.DialOption{grpc.WithInsecure()}, nil
}

func GetInfo() authplugin.Info {
	return authplugin.Info{
		Name:    "NONE",
		GetAuth: returnAuth,
	}
}
