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

package attribute

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	accessLogGRPC "github.com/envoyproxy/go-control-plane/envoy/service/accesslog/v2"
	authzGRPC "github.com/envoyproxy/go-control-plane/envoy/service/auth/v2"
	"github.com/golang/protobuf/ptypes/timestamp"

	mixerpb "istio.io/api/mixer/v1"
	attr "istio.io/pkg/attribute"
)


// EnvoyProtoBag implements the Bag interface on top of an Attributes proto.
type EnvoyProtoBag struct {
	reqMap              map[string]interface{}
	globalDict          map[string]int32
	globalWordList      []string
	messageDict         map[string]int32
	convertedStringMaps map[int32]attr.StringMap
	stringMapMutex      sync.RWMutex

	// to keep track of attributes that are referenced
	referencedAttrs      map[attr.Reference]attr.Presence
	referencedAttrsMutex sync.Mutex
}


var envoyProtoBags = sync.Pool{
	New: func() interface{} {
		return &EnvoyProtoBag{
			referencedAttrs: make(map[attr.Reference]attr.Presence, referencedAttrsSize),
		}
	},
}

func fillAddress(reqMap map[string]interface{}, address *core.Address, name string) {
	reqMap[name+".ip"] = []byte(net.ParseIP(address.GetSocketAddress().GetAddress()).To16())
	reqMap[name+".port"] = int64(address.GetSocketAddress().GetPortValue())
}

func reformatTime(theTime *timestamp.Timestamp, durationNanos int32) time.Time {
	return time.Unix(theTime.Seconds, int64(theTime.Nanos)+int64(durationNanos))
}

// AuthzProtoBag returns an attribute bag for an Ext-Authz Check Request.
// When you are done using the proto bag, call the Done method to recycle it.
func AuthzProtoBag(req *authzGRPC.CheckRequest) *EnvoyProtoBag {
	pb := envoyProtoBags.Get().(*EnvoyProtoBag)

	// build the message-level dictionary
	reqMap := make(map[string]interface{})
	//TODO account for other protocols
	reqMap["context.protocol"] = "http"
	reqMap["context.reporter.kind"] = "inbound"
	fillAddress(reqMap, req.GetAttributes().GetDestination().GetAddress(), "destination")
	fillAddress(reqMap, req.GetAttributes().GetSource().GetAddress(), "source")
	reqMap["destination.principal"] = req.GetAttributes().GetDestination().GetPrincipal()
	reqMap["source.principal"] = req.GetAttributes().GetSource().GetPrincipal()
	reqMap["request.headers"] = attr.WrapStringMap(req.GetAttributes().GetRequest().GetHttp().GetHeaders())
	reqMap["request.host"] = req.GetAttributes().GetRequest().GetHttp().GetHost()
	reqMap["request.method"] = req.GetAttributes().GetRequest().GetHttp().GetMethod()
	reqMap["request.path"] = req.GetAttributes().GetRequest().GetHttp().GetPath()
	reqMap["request.scheme"] = req.GetAttributes().GetRequest().GetHttp().GetScheme()
	reqMap["request.time"] = reformatTime(req.GetAttributes().GetRequest().GetTime(), 0)
	reqMap["request.useragent"] = req.GetAttributes().GetRequest().GetHttp().GetHeaders()["user-agent"]

	pb.reqMap = reqMap

	scope.Debugf("Returning bag with attributes:\n%v", pb)

	return pb
}

// AccessLogProtoBag returns an attribute bag from a StreamAccessLogsMessage
// When you are done using the proto bag, call the Done method to recycle it.
//num is the index of the entry from the message's batch to create a bag from
func AccessLogProtoBag(msg *accessLogGRPC.StreamAccessLogsMessage, num int) *EnvoyProtoBag {
	// build the message-level dictionary
	pb := envoyProtoBags.Get().(*EnvoyProtoBag)
	reqMap := make(map[string]interface{})
	if httpLogs := msg.GetHttpLogs(); httpLogs != nil {
		reqMap["context.protocol"] = "http"
		reqMap["context.reporter.kind"] = "inbound"
		fillAddress(reqMap, httpLogs.GetLogEntry()[num].GetCommonProperties().GetDownstreamLocalAddress(),
			"destination")
		fillAddress(reqMap, httpLogs.GetLogEntry()[num].GetCommonProperties().GetDownstreamRemoteAddress(), "source")
		reqMap["destination.principal"] = httpLogs.GetLogEntry()[num].GetCommonProperties().GetTlsProperties().
			GetLocalCertificateProperties().GetSubjectAltName()[0].GetUri()
		reqMap["source.principal"] = httpLogs.GetLogEntry()[num].GetCommonProperties().GetTlsProperties().
			GetPeerCertificateProperties().GetSubjectAltName()[0].GetUri()
		reqMap["request.headers"] = attr.WrapStringMap(httpLogs.GetLogEntry()[num].GetRequest().GetRequestHeaders())
		reqMap["request.host"] = httpLogs.GetLogEntry()[num].GetRequest().GetAuthority()
		reqMap["request.method"] = httpLogs.GetLogEntry()[num].GetRequest().GetRequestMethod().String()
		reqMap["request.path"] = httpLogs.GetLogEntry()[num].GetRequest().GetPath()
		reqMap["request.scheme"] = httpLogs.GetLogEntry()[num].GetRequest().GetScheme()
		reqMap["request.time"] = reformatTime(httpLogs.GetLogEntry()[num].GetCommonProperties().GetStartTime(), 0)
		//is this the right time
		reqMap["response.time"] = reformatTime(httpLogs.GetLogEntry()[num].GetCommonProperties().GetStartTime(),
			httpLogs.GetLogEntry()[num].GetCommonProperties().GetTimeToFirstUpstreamRxByte().Nanos)
		reqMap["request.useragent"] = httpLogs.GetLogEntry()[num].GetRequest().GetUserAgent()
		reqMap["response.headers"] = attr.WrapStringMap(httpLogs.GetLogEntry()[num].GetResponse().GetResponseHeaders())
		reqMap["response.code"] = int64(httpLogs.GetLogEntry()[num].GetResponse().GetResponseCode().GetValue())
		reqMap["response.size"] = int64(httpLogs.GetLogEntry()[num].GetResponse().GetResponseBodyBytes())
		reqMap["response.total_size"] = int64(httpLogs.GetLogEntry()[num].GetResponse().
			GetResponseBodyBytes()) + int64(httpLogs.GetLogEntry()[num].GetResponse().GetResponseHeadersBytes())
		reqMap["UPSTREAM_CLUSTER"] = httpLogs.GetLogEntry()[num].GetCommonProperties().GetUpstreamCluster()
		//reqMap["context.proxy_error_code"] = httpLogs.GetLogEntry()[num].GetCommonProperties().GetResponseFlags()
		reqMap["connection.requested_server_name"] = httpLogs.GetLogEntry()[num].GetCommonProperties().GetTlsProperties().GetTlsSniHostname()

	} else if tcpLogs := msg.GetTcpLogs(); tcpLogs != nil {
		reqMap["context.protocol"] = "tcp"
		reqMap["context.reporter.kind"] = "inbound"
		fillAddress(reqMap, tcpLogs.GetLogEntry()[num].GetCommonProperties().GetDownstreamLocalAddress(), "destination")
		fillAddress(reqMap, tcpLogs.GetLogEntry()[num].GetCommonProperties().GetDownstreamRemoteAddress(), "source")
		reqMap["request.time"] = reformatTime(tcpLogs.GetLogEntry()[num].GetCommonProperties().GetStartTime(), 0)
		reqMap["response.time"] = reformatTime(tcpLogs.GetLogEntry()[num].GetCommonProperties().GetStartTime(),
			tcpLogs.GetLogEntry()[num].GetCommonProperties().GetTimeToFirstUpstreamRxByte().Nanos)
		reqMap["connection.requested_server_name"] = tcpLogs.GetLogEntry()[num].GetCommonProperties().
			GetTlsProperties().GetTlsSniHostname()
		reqMap["connection.received.bytes"] = tcpLogs.GetLogEntry()[num].GetConnectionProperties().GetReceivedBytes()
		reqMap["connection.sent.bytes"] = tcpLogs.GetLogEntry()[num].GetConnectionProperties().GetSentBytes()
		reqMap["UPSTREAM_CLUSTER"] = tcpLogs.GetLogEntry()[num].GetCommonProperties().GetUpstreamCluster()
		//reqMap["context.proxy_error_code"] = tcpLogs.GetLogEntry()[num].GetCommonProperties().GetResponseFlags()
		//needs to be converted to proper format
	}

	pb.reqMap = reqMap
	scope.Debugf("Returning bag with attributes:\n%v", pb)
	return pb
}

//fills in destination.service.name and destination.service.host after the initial bag has been built
func (pb *EnvoyProtoBag) AddNamespaceDependentAttributes(destinationNamespace string) {
	upstreamCluster := pb.reqMap["UPSTREAM_CLUSTER"]
	parts := strings.Split(fmt.Sprintf("%v", upstreamCluster), "|")
	var host string
	if len(parts) == 4 {
		host = parts[3]
	} else {
		host = fmt.Sprintf("%v", pb.reqMap["request.host"])
	}
	pb.reqMap["destination.service.host"] = host
	namePos := strings.IndexAny(host, ".:")
	if namePos == -1 {
		pb.reqMap["destination.service.name"] = host
		return
	} else if host[namePos] == ':' {
		pb.reqMap["destination.service.name"] = host[0:namePos]
		return
	}
	namespacePos := strings.IndexAny(host[namePos + 1:], ".:")
	var serviceNamespace string
	if namespacePos == -1 {
		serviceNamespace = host[namePos:]
	} else {
		serviceNamespace = host[namePos + 1:namespacePos + namePos + 1]
	}
	if serviceNamespace == destinationNamespace {
		pb.reqMap["destination.service.name"] = host[0:namePos]
	} else {
		pb.reqMap["destination.service.name"] = host
	}
}


// Get returns an attribute value.
func (pb *EnvoyProtoBag) Get(name string) (interface{}, bool) {
	if val, ok := pb.reqMap[name]; ok {
		pb.Reference(name, attr.Exact)
		return val, ok
	}
	pb.Reference(name, attr.Absence)
	return nil, false
}

// ReferenceTracker for a proto bag
func (pb *EnvoyProtoBag) ReferenceTracker() attr.ReferenceTracker {
	return pb
}

// GetReferencedAttributes returns the set of attributes that have been referenced through this bag.
func (pb *EnvoyProtoBag) GetReferencedAttributes(globalDict map[string]int32,
	globalWordCount int) *mixerpb.ReferencedAttributes {
	output := &mixerpb.ReferencedAttributes{}

	ds := newDictState(globalDict, globalWordCount)

	output.AttributeMatches = make([]mixerpb.ReferencedAttributes_AttributeMatch, len(pb.referencedAttrs))
	i := 0
	for k, v := range pb.referencedAttrs {
		mk := int32(0)
		if len(k.MapKey) > 0 {
			mk = ds.assignDictIndex(k.MapKey)
		}
		output.AttributeMatches[i] = mixerpb.ReferencedAttributes_AttributeMatch{
			Name:      ds.assignDictIndex(k.Name),
			MapKey:    mk,
			Condition: mixerpb.ReferencedAttributes_Condition(v),
		}
		i++
	}

	output.Words = ds.getMessageWordList()

	return output
}

// Clear the list of referenced attributes being tracked by this bag
func (pb *EnvoyProtoBag) Clear() {
	for k := range pb.referencedAttrs {
		delete(pb.referencedAttrs, k)
	}
}

// Restore the list of referenced attributes being tracked by this bag
func (pb *EnvoyProtoBag) Restore(snap attr.ReferencedAttributeSnapshot) {
	ra := make(map[attr.Reference]attr.Presence, len(snap.ReferencedAttrs))
	for k, v := range snap.ReferencedAttrs {
		ra[k] = v
	}
	pb.referencedAttrs = ra
}

// Snapshot grabs a snapshot of the currently referenced attributes
func (pb *EnvoyProtoBag) Snapshot() attr.ReferencedAttributeSnapshot {
	var snap attr.ReferencedAttributeSnapshot

	pb.referencedAttrsMutex.Lock()
	snap.ReferencedAttrs = make(map[attr.Reference]attr.Presence, len(pb.referencedAttrs))
	for k, v := range pb.referencedAttrs {
		snap.ReferencedAttrs[k] = v
	}
	pb.referencedAttrsMutex.Unlock()
	return snap
}

func (pb *EnvoyProtoBag) MapReference(name string, key string, condition attr.Presence) {
	pb.referencedAttrsMutex.Lock()
	pb.referencedAttrs[attr.Reference{Name: name, MapKey: key}] = condition
	pb.referencedAttrsMutex.Unlock()
}

func (pb *EnvoyProtoBag) Reference(name string, condition attr.Presence) {
	pb.referencedAttrsMutex.Lock()
	pb.referencedAttrs[attr.Reference{Name: name}] = condition
	pb.referencedAttrsMutex.Unlock()
}

// Contains returns true if protobag contains this key.
func (pb *EnvoyProtoBag) Contains(key string) bool {
	if _, ok := pb.reqMap[key]; ok {
		return true
	}
	return false

}

// Names returns the names of all the attributes known to this bag.
func (pb *EnvoyProtoBag) Names() []string {
	keys := make([]string, 0, len(pb.reqMap))
	for k := range pb.reqMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}

// Done indicates the bag can be reclaimed.
func (pb *EnvoyProtoBag) Done() {
	pb.Reset()
	envoyProtoBags.Put(pb)
}

// Reset removes all local state.
func (pb *EnvoyProtoBag) Reset() {
	pb.globalDict = make(map[string]int32)
	pb.globalWordList = nil
	pb.messageDict = make(map[string]int32)
	pb.stringMapMutex.Lock()
	pb.convertedStringMaps = make(map[int32]attr.StringMap)
	pb.stringMapMutex.Unlock()
	pb.referencedAttrsMutex.Lock()
	pb.referencedAttrs = make(map[attr.Reference]attr.Presence, referencedAttrsSize)
	pb.referencedAttrsMutex.Unlock()
}

// String runs through the named attributes, looks up their values,
// and prints them to a string.
func (pb *EnvoyProtoBag) String() string {
	var sb strings.Builder
	for _, key := range pb.Names() {
		val, _ := pb.Get(key)
		sb.WriteString(fmt.Sprintf("%v : %v\n", key, val))
	}
	return sb.String()
}
