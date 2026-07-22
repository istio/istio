// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package kube

import (
	"maps"
	"reflect"
	"sort"
	"strconv"

	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pilot/pkg/model"
	destinationmodel "istio.io/istio/pilot/pkg/model/destination"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/visibility"
)

// ServiceDestinationIR is the dual-run projection of a Kubernetes Service.
// Frontend retains addressability while Definitions contain only outbound
// destination intent. This helper has no controller or registry side effects.
type ServiceDestinationIR struct {
	Frontend    destinationmodel.FrontendDefinition
	Definitions []destinationmodel.DestinationDefinition
}

func (s ServiceDestinationIR) ResourceName() string { return s.Frontend.ID.String() }

func (s ServiceDestinationIR) Equals(other ServiceDestinationIR) bool {
	return reflect.DeepEqual(s, other)
}

// ConvertServiceToDestinationIR splits the existing Kubernetes conversion
// into frontend and per-port destination intent. It deliberately derives from
// ConvertService during dual-run so source-specific semantics cannot drift.
func ConvertServiceToDestinationIR(
	svc corev1.Service,
	nsAnnotations map[string]string,
	domainSuffix string,
	clusterID cluster.ID,
	trustDomain string,
) ServiceDestinationIR {
	legacy := ConvertService(svc, nsAnnotations, domainSuffix, clusterID, trustDomain)
	source := model.ConfigKey{Kind: kind.Service, Namespace: svc.Namespace, Name: svc.Name}
	ports := make([]destinationmodel.DestinationPort, 0, len(legacy.Ports))
	definitions := make([]destinationmodel.DestinationDefinition, 0, len(legacy.Ports))
	defaultDestinations := make([]destinationmodel.DefinitionID, 0, len(legacy.Ports))
	for _, legacyPort := range legacy.Ports {
		port := destinationmodel.DestinationPort{Name: legacyPort.Name, Number: legacyPort.Port, Protocol: legacyPort.Protocol}
		portIdentity := legacyPort.Name
		if portIdentity == "" {
			portIdentity = legacyPort.Protocol.String() + ":" + strconv.Itoa(legacyPort.Port)
		}
		id := destinationmodel.DefinitionID{Source: source, UID: string(svc.UID), Port: portIdentity}
		endpointSource := destinationmodel.EndpointSource{
			Kind: destinationmodel.ServiceMembership, Source: source, Port: uint32(legacyPort.Port), Extension: string(clusterID),
		}
		if legacy.Resolution == model.Alias {
			endpointSource.Kind = destinationmodel.DNS
			endpointSource.Hostname = host.Name(legacy.Attributes.ExternalName)
		}
		ports = append(ports, port)
		defaultDestinations = append(defaultDestinations, id)
		definitions = append(definitions, destinationmodel.DestinationDefinition{
			ID: id, Namespace: svc.Namespace, Ports: []destinationmodel.DestinationPort{port}, Endpoints: endpointSource,
			Connection:   destinationmodel.ConnectionPolicy{Protocol: legacyPort.Protocol},
			Metadata:     destinationmodel.DestinationMetadata{ServiceAccounts: append([]string(nil), legacy.ServiceAccounts...)},
			MeshExternal: legacy.MeshExternal, Dependencies: []model.ConfigKey{source},
			CreationTime: legacy.CreationTime, Version: legacy.ResourceVersion,
		})
	}
	exportTo := make([]visibility.Instance, 0, len(legacy.Attributes.ExportTo))
	for item := range legacy.Attributes.ExportTo {
		exportTo = append(exportTo, item)
	}
	sort.Slice(exportTo, func(i, j int) bool { return exportTo[i] < exportTo[j] })
	return ServiceDestinationIR{
		Frontend: destinationmodel.FrontendDefinition{
			ID: source, UID: string(svc.UID), Hostname: legacy.Hostname, ClusterVIPs: *legacy.ClusterVIPs.DeepCopy(),
			DefaultAddress: legacy.DefaultAddress, Ports: ports, DefaultDestinations: defaultDestinations,
			Resolution: legacy.Resolution, MeshExternal: legacy.MeshExternal, ExternalName: legacy.Attributes.ExternalName,
			ServiceAccounts: append([]string(nil), legacy.ServiceAccounts...), ExportTo: exportTo,
			Selector: maps.Clone(legacy.Attributes.LabelSelectors), Type: legacy.Attributes.Type,
			ClusterExternalAddresses: *legacy.Attributes.ClusterExternalAddresses.DeepCopy(),
			ClusterExternalPorts:     cloneExternalPorts(legacy.Attributes.ClusterExternalPorts), NodeLocal: legacy.Attributes.NodeLocal,
			PublishNotReadyAddresses: legacy.Attributes.PublishNotReadyAddresses,
			CreationTime:             legacy.CreationTime, Version: legacy.ResourceVersion,
		},
		Definitions: definitions,
	}
}

func cloneExternalPorts(in map[cluster.ID]map[uint32]uint32) map[cluster.ID]map[uint32]uint32 {
	if in == nil {
		return nil
	}
	out := make(map[cluster.ID]map[uint32]uint32, len(in))
	for id, ports := range in {
		out[id] = maps.Clone(ports)
	}
	return out
}
