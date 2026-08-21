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

// Package destination contains the source-neutral intermediate representation
// for outbound destinations. Definitions do not imply an addressable frontend;
// only an activated Binding is eligible for runtime projection.
package destination

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/visibility"
)

// DefinitionID identifies a source-defined destination across updates. UID is
// the delete/recreate boundary when the source provides one. Port is the
// source's stable port identity, not necessarily its display name.
type DefinitionID struct {
	Source model.ConfigKey
	UID    string
	Port   string
}

func (i DefinitionID) String() string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", i.Source.Kind, i.Source.Namespace, i.Source.Name, i.UID, i.Port)
}

// ConsumerID is a reusable consumer scope. Kind is intentionally an ordinary
// string so source adapters can introduce scopes without changing this core
// contract (for example Gateway, Waypoint, SidecarScope, or Mesh).
type ConsumerID struct {
	Kind      string
	Namespace string
	Name      string
	Cluster   cluster.ID
}

func (i ConsumerID) String() string {
	return strings.Join([]string{i.Kind, i.Namespace, i.Name, string(i.Cluster)}, "/")
}

type BindingKey struct {
	Definition DefinitionID
	Consumer   ConsumerID
	Policy     string
}

type AuthorizationResult string

const (
	AuthorizationAllowed AuthorizationResult = "Allowed"
	AuthorizationDenied  AuthorizationResult = "Denied"
)

// ReferenceEdge records the consumer reference which activates a definition.
// Authorization describes object-reference authorization only; credential and
// traffic authorization remain independent policy decisions.
type ReferenceEdge struct {
	Consumer    ConsumerID
	Referencer  model.ConfigKey
	Destination DefinitionID
	Port        string
	Grant       AuthorizationResult
}

func (k BindingKey) String() string {
	parts := []string{k.Definition.String(), k.Consumer.String()}
	if k.Policy != "" {
		parts = append(parts, k.Policy)
	}
	return strings.Join(parts, "/")
}

type DestinationPort struct {
	Name     string
	Number   int
	Protocol protocol.Instance
}

// EndpointSourceKind is closed by the constants below. EndpointSource is a
// comparable descriptor; resolvers own endpoint data and source-specific
// watches rather than embedding mutable endpoint lists in krt objects.
type EndpointSourceKind string

const (
	ServiceMembership EndpointSourceKind = "ServiceMembership"
	StaticEndpoints   EndpointSourceKind = "StaticEndpoints"
	DNS               EndpointSourceKind = "DNS"
	DynamicDNS        EndpointSourceKind = "DynamicDNS"
	ExtensionResolved EndpointSourceKind = "ExtensionResolved"
)

type EndpointSource struct {
	Kind      EndpointSourceKind
	Source    model.ConfigKey
	Hostname  host.Name
	Port      uint32
	Extension string
}

type TLSMode string

const (
	TLSDisabled TLSMode = "Disabled"
	TLSSimple   TLSMode = "Simple"
	TLSMutual   TLSMode = "Mutual"
)

type CredentialReference struct {
	Resource model.ConfigKey
	Section  string
}

type TLSSettings struct {
	Mode              TLSMode
	SNI               string
	SubjectAltNames   []string
	Validation        *CredentialReference
	ClientCertificate *CredentialReference
}

type ConnectionPolicy struct {
	Protocol protocol.Instance
	TLS      TLSSettings
	// PolicyKey is a normalized attachment identity used in BindingKey and
	// dependency tracking while richer traffic policy fields are introduced.
	PolicyKey string
}

// DestinationMetadata carries stable source-neutral identities. Extension is
// a resolver/compiler discriminator, not an untyped API payload.
type DestinationMetadata struct {
	ServiceAccounts []string
	Network         string
	Extension       string
	Semantics       Semantics
	FailureMode     ExtensionFailureMode
}

type Semantics string

const (
	StandardSemantics      Semantics = "Standard"
	InferencePoolSemantics Semantics = "InferencePool"
)

type ExtensionFailureMode string

const (
	ExtensionFailClose ExtensionFailureMode = "FailClose"
	ExtensionFailOpen  ExtensionFailureMode = "FailOpen"
)

type DestinationDefinition struct {
	ID           DefinitionID
	Namespace    string
	Ports        []DestinationPort
	Endpoints    EndpointSource
	Connection   ConnectionPolicy
	Metadata     DestinationMetadata
	MeshExternal bool
	Dependencies []model.ConfigKey
	CreationTime time.Time
	Version      string
}

func (d DestinationDefinition) ResourceName() string { return d.ID.String() }

func (d DestinationDefinition) Equals(other DestinationDefinition) bool {
	return reflect.DeepEqual(d, other)
}

// FrontendDefinition is addressable source intent. Unlike a destination, it
// may participate in VIP/DNS capture and policy attachment. DefaultDestinations
// maps each frontend port to the destination used when no route overrides it.
type FrontendDefinition struct {
	Key                      string
	ID                       model.ConfigKey
	UID                      string
	Hostname                 host.Name
	Addresses                []string
	ClusterVIPs              model.AddressMap
	DefaultAddress           string
	Ports                    []DestinationPort
	DefaultDestinations      []DefinitionID
	Resolution               model.Resolution
	MeshExternal             bool
	ExternalName             string
	ServiceAccounts          []string
	SubjectAltNames          []string
	ExportTo                 []visibility.Instance
	Selector                 map[string]string
	Type                     string
	ClusterExternalAddresses model.AddressMap
	ClusterExternalPorts     map[cluster.ID]map[uint32]uint32
	NodeLocal                bool
	PublishNotReadyAddresses bool
	CreationTime             time.Time
	Version                  string
}

func (f FrontendDefinition) ResourceName() string {
	if f.Key != "" {
		return f.Key
	}
	return f.ID.String() + "/" + f.Hostname.String() + "/" + f.DefaultAddress
}

func (f FrontendDefinition) Equals(other FrontendDefinition) bool {
	return reflect.DeepEqual(f, other)
}

// ConsumerSet describes visibility without expanding it to individual proxy
// identities. Empty Kind means visibility is limited to Binding.Consumer.
type ConsumerSet struct {
	Kind      string
	Namespace string
	Name      string
}

type DestinationBinding struct {
	Key          BindingKey
	RuntimeName  host.Name
	Definition   DefinitionID
	Consumer     ConsumerID
	Port         DestinationPort
	Endpoints    EndpointSource
	Connection   ConnectionPolicy
	Visibility   ConsumerSet
	Dependencies []model.ConfigKey
	Namespace    string
	CreationTime time.Time
}

func (b DestinationBinding) ResourceName() string { return b.Key.String() }

func (b DestinationBinding) Equals(other DestinationBinding) bool {
	return reflect.DeepEqual(b, other)
}

// NormalizeDependencies returns a stable, duplicate-free dependency list.
func NormalizeDependencies(in ...model.ConfigKey) []model.ConfigKey {
	seen := make(map[model.ConfigKey]struct{}, len(in))
	for _, key := range in {
		seen[key] = struct{}{}
	}
	out := make([]model.ConfigKey, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out
}
