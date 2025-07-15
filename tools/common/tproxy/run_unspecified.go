//go:build !linux
// +build !linux

// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package tproxy

import (
	"errors"

	"istio.io/istio/tools/common/config"
)

// ErrNotImplemented is returned when a requested feature is not implemented.
var ErrNotImplemented = errors.New("not implemented")

// configureTProxyRoutes configures ip firewall rules to enable TPROXY support.
// See https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/original_src_filter
// nolint:unused
func configureTProxyRoutes(_ *config.Config) error {
	return ErrNotImplemented
}

func ConfigureRoutes(_ *config.Config) error {
	return ErrNotImplemented
}
