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

package config

// RulesKind defines the config kind Name of mixer Rules.
const RulesKind = "rule"

// AttributeManifestKind define the config kind Name of attribute manifests.
const AttributeManifestKind = "attributemanifest"

// ContextProtocolTCP defines constant for tcp protocol.
const ContextProtocolTCP = "tcp"

// ContextProtocolAttributeName is the attribute that defines the protocol context.
const ContextProtocolAttributeName = "context.protocol"

const istioProtocol = "istio-protocol"
