// Copyright 2017 Istio Authors
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

package cmd

import (
	"istio.io/mixer/cmd/shared"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/template"
)

var defaultSeverArgs = serverArgs{
	maxMessageSize:                1024 * 1024,
	maxConcurrentStreams:          256,
	apiWorkerPoolSize:             256,
	adapterWorkerPoolSize:         256,
	expressionEvalCacheSize:       expr.DefaultCacheSize,
	configAPIPort:                 0,
	monitoringPort:                0,
	singleThreaded:                false,
	compressedPayload:             false,
	traceOutput:                   "",
	serverCertFile:                "",
	serverKeyFile:                 "",
	clientCertFiles:               "",
	configStoreURL:                "",
	configStore2URL:               "",
	configDefaultNamespace:        "",
	configFetchIntervalSec:        3,
	configIdentityAttribute:       "target.service",
	configIdentityAttributeDomain: "",
	useAst: false,
}

// SetupTestServer sets up a test server environment
func SetupTestServer(info map[string]template.Info, adapters []adapter.InfoFn, legacyAdapters []adapter.RegisterFn,
	configStoreURL string, configStore2URL string, configDefaultNamespace string, configIdentityAttribute,
	configIdentityAttributeDomain string, useAst bool) *ServerContext {
	sa := defaultSeverArgs
	sa.configStoreURL = configStoreURL
	sa.configStore2URL = configStore2URL
	sa.configDefaultNamespace = configDefaultNamespace
	sa.configIdentityAttribute = configIdentityAttribute
	sa.configIdentityAttributeDomain = configIdentityAttributeDomain
	sa.useAst = useAst
	return setupServer(&sa, info, adapters, legacyAdapters, shared.Printf, shared.Fatalf)
}
