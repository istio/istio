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

package main

import (
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/bootstrap"
	"istio.io/istio/pkg/config"
	"istio.io/pkg/env"
)

var (
	tlsServerCertChain = env.RegisterStringVar(bootstrap.IstioMetaPrefix+model.NodeMetadataTLSServerCertChain, config.DefaultCertChain, "").Get()
	tlsServerKey       = env.RegisterStringVar(bootstrap.IstioMetaPrefix+model.NodeMetadataTLSServerKey, config.DefaultKey, "").Get()
	tlsServerRootCert  = env.RegisterStringVar(bootstrap.IstioMetaPrefix+model.NodeMetadataTLSServerRootCert, config.DefaultRootCert, "").Get()

	tlsClientCertChain = env.RegisterStringVar(bootstrap.IstioMetaPrefix+model.NodeMetadataTLSClientCertChain, config.DefaultCertChain, "").Get()
	tlsClientKey       = env.RegisterStringVar(bootstrap.IstioMetaPrefix+model.NodeMetadataTLSClientKey, config.DefaultKey, "").Get()
	tlsClientRootCert  = env.RegisterStringVar(bootstrap.IstioMetaPrefix+model.NodeMetadataTLSClientRootCert, config.DefaultRootCert, "").Get()
)
