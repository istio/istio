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

package serviceentry

import (
	"fmt"
	"sort"
	"strings"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/util/sets"
)

type ConflictingServiceEntryProtocolAnalyzer struct{}

var _ analysis.Analyzer = &ConflictingServiceEntryProtocolAnalyzer{}

type scopedHostPortKey struct {
	scope string
	host  string
	port  uint32
}

type protoEntry struct {
	protocol string
	instance *resource.Instance
}

func (c *ConflictingServiceEntryProtocolAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "serviceentry.ConflictingServiceEntryProtocolAnalyzer",
		Description: "Checks if multiple ServiceEntries define the same host and port with different protocols",
		Inputs: []config.GroupVersionKind{
			gvk.ServiceEntry,
		},
	}
}

func (c *ConflictingServiceEntryProtocolAnalyzer) Analyze(ctx analysis.Context) {
	index := make(map[scopedHostPortKey][]protoEntry)

	ctx.ForEach(gvk.ServiceEntry, func(r *resource.Instance) bool {
		se := r.Message.(*v1alpha3.ServiceEntry)
		seNamespace := r.Metadata.FullName.Namespace.String()

		scopes := exportToScopes(se.ExportTo, seNamespace)
		for scope := range scopes {
			for _, h := range se.Hosts {
				for _, p := range se.Ports {
					key := scopedHostPortKey{scope: scope, host: h, port: p.Number}
					index[key] = append(index[key], protoEntry{protocol: p.Protocol, instance: r})
				}
			}
		}
		return true
	})

	reported := make(map[resource.FullName]bool)

	for key, entries := range index {
		if key.scope != util.ExportToAllNamespaces {
			starKey := scopedHostPortKey{scope: util.ExportToAllNamespaces, host: key.host, port: key.port}
			entries = append(entries, index[starKey]...)
		}

		protocols := make(map[string]bool)
		for _, e := range entries {
			protocols[strings.ToUpper(e.protocol)] = true
		}
		if len(protocols) < 2 {
			continue
		}

		seenNames := make(map[string]bool)
		names := make([]string, 0)
		for _, e := range entries {
			n := e.instance.Metadata.FullName.String()
			if !seenNames[n] {
				seenNames[n] = true
				names = append(names, n)
			}
		}
		sort.Strings(names)
		namesStr := strings.Join(names, ", ")

		protoList := make([]string, 0, len(protocols))
		for p := range protocols {
			protoList = append(protoList, p)
		}
		sort.Strings(protoList)
		protocolsStr := strings.Join(protoList, ", ")

		for _, e := range entries {
			if reported[e.instance.Metadata.FullName] {
				continue
			}
			reported[e.instance.Metadata.FullName] = true
			m := msg.NewConflictingServiceEntryProtocol(e.instance, namesStr, key.host, int(key.port), protocolsStr)
			if line, ok := util.ErrorLine(e.instance, fmt.Sprintf(util.MetadataName)); ok {
				m.Line = line
			}
			ctx.Report(gvk.ServiceEntry, m)
		}
	}
}

func exportToScopes(exportTo []string, seNamespace string) sets.String {
	if util.IsExportToAllNamespaces(exportTo) {
		return sets.New(util.ExportToAllNamespaces)
	}
	scopes := sets.New[string]()
	for _, et := range exportTo {
		if et == util.ExportToNamespaceLocal {
			scopes.Insert(seNamespace)
		} else {
			scopes.Insert(et)
		}
	}
	return scopes
}
