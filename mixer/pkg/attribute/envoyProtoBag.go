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
	alspb "github.com/envoyproxy/go-control-plane/envoy/data/accesslog/v2"
	accesslog "github.com/envoyproxy/go-control-plane/envoy/service/accesslog/v2"
	authz "github.com/envoyproxy/go-control-plane/envoy/service/auth/v2"
	"github.com/golang/protobuf/ptypes/timestamp"

	mixerpb "istio.io/api/mixer/v1"
	attr "istio.io/pkg/attribute"
)

// EnvoyProtoBag implements the Bag interface on top of an Attributes proto.
type EnvoyProtoBag struct {
	reqMap          map[string]interface{}
	upstreamCluster string

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
	if address == nil {
		return
	}
	if socketaddress := address.GetSocketAddress(); socketaddress != nil {
		reqMap[name+".ip"] = []byte(net.ParseIP(socketaddress.GetAddress()).To16())
		reqMap[name+".port"] = int64(socketaddress.GetPortValue())
	}
}

func reformatTime(theTime *timestamp.Timestamp, durationNanos int32) time.Time {
	return time.Unix(theTime.Seconds, int64(theTime.Nanos)+int64(durationNanos))
}

func isGrpc(headers map[string]string) bool {
	if contentType, ok := headers["content-type"]; ok {
		return strings.HasPrefix(contentType, "application/grpc")
	}
	return false
}

func fillContextProtocol(reqMap map[string]interface{}, headers map[string]string) {
	if isGrpc(headers) {
		reqMap["context.protocol"] = "grpc"
	} else {
		reqMap["context.protocol"] = "http"
	}
}

func stripPrincipal(principal string) string {
	spif := "spiffe://"
	if strings.HasPrefix(principal, spif) {
		return principal[len(spif):]
	}
	return principal
}

// AuthzProtoBag returns an attribute bag for an Ext-Authz Check Request.
// When you are done using the proto bag, call the Done method to recycle it.
func AuthzProtoBag(req *authz.CheckRequest) *EnvoyProtoBag {
	pb := envoyProtoBags.Get().(*EnvoyProtoBag)
	reqMap := make(map[string]interface{})
	reqMap["context.reporter.kind"] = "inbound"
	if attributes := req.GetAttributes(); attributes != nil {
		if destination := attributes.GetDestination(); destination != nil {
			fillAddress(reqMap, destination.GetAddress(), "destination")
			reqMap["destination.principal"] = stripPrincipal(destination.GetPrincipal())
		}
		if source := attributes.GetSource(); source != nil {
			fillAddress(reqMap, source.GetAddress(), "source")
			reqMap["source.principal"] = stripPrincipal(source.GetPrincipal())
		}
		if request := attributes.GetRequest(); request != nil {
			if reqtime := request.GetTime(); reqtime != nil {
				reqMap["request.time"] = reformatTime(reqtime, 0)
			}
			if http := request.GetHttp(); http != nil {
				reqMap["request.headers"] = attr.WrapStringMap(http.GetHeaders())
				reqMap["request.host"] = http.GetHost()
				reqMap["request.method"] = http.GetMethod()
				reqMap["request.path"] = http.GetPath()
				reqMap["request.scheme"] = http.GetScheme()
				reqMap["request.useragent"] = http.GetHeaders()["user-agent"]
				fillContextProtocol(reqMap, http.GetHeaders())
			}
		}
	}
	pb.reqMap = reqMap

	scope.Debugf("Returning bag with attributes:\n%v", pb)

	return pb
}

// AccessLogProtoBag returns an attribute bag from a StreamAccessLogsMessage
// When you are done using the proto bag, call the Done method to recycle it.
// num is the index of the entry from the message's batch to create a bag from
func AccessLogProtoBag(msg *accesslog.StreamAccessLogsMessage, num int) *EnvoyProtoBag {
	pb := envoyProtoBags.Get().(*EnvoyProtoBag)
	reqMap := make(map[string]interface{})
	var commonproperties alspb.AccessLogCommon

	if httpLogs := msg.GetHttpLogs(); httpLogs != nil {
		//default protocol to start but if it's grpc it will be overwritten
		reqMap["context.protocol"] = "http"
		commonproperties = *httpLogs.GetLogEntry()[num].GetCommonProperties()
		if starttime := commonproperties.GetStartTime(); starttime != nil {
			reqMap["request.time"] = reformatTime(starttime, 0)
			if timetoupbyte := commonproperties.GetTimeToFirstUpstreamRxByte(); timetoupbyte != nil {
				reqMap["response.time"] = reformatTime(starttime, timetoupbyte.Nanos)
			}
		}

		if request := httpLogs.GetLogEntry()[num].GetRequest(); request != nil {
			reqMap["request.host"] = request.GetAuthority()
			reqMap["request.method"] = request.GetRequestMethod().String()
			reqMap["request.path"] = request.GetPath()
			reqMap["request.scheme"] = request.GetScheme()
			reqMap["request.useragent"] = request.GetUserAgent()
			if reqheaders := request.GetRequestHeaders(); reqheaders != nil {
				reqMap["request.headers"] = attr.WrapStringMap(reqheaders)
				fillContextProtocol(reqMap, reqheaders)
			}
		}

		if response := httpLogs.GetLogEntry()[num].GetResponse(); response != nil {
			reqMap["response.headers"] = attr.WrapStringMap(response.GetResponseHeaders())
			reqMap["response.code"] = int64(response.GetResponseCode().GetValue())
			reqMap["response.size"] = int64(response.GetResponseBodyBytes())
			reqMap["response.total_size"] = int64(response.GetResponseBodyBytes()) + int64(response.GetResponseHeadersBytes())
		}
	} else if tcpLogs := msg.GetTcpLogs(); tcpLogs != nil {
		reqMap["context.protocol"] = "tcp"
		commonproperties = *tcpLogs.GetLogEntry()[num].GetCommonProperties()
		if startTime := commonproperties.GetStartTime(); startTime != nil {
			reqMap["context.time"] = reformatTime(startTime, 0)
		}

		if connection := tcpLogs.GetLogEntry()[num].GetConnectionProperties(); connection != nil {
			reqMap["connection.received.bytes"] = int64(connection.GetReceivedBytes())
			reqMap["connection.sent.bytes"] = int64(connection.GetSentBytes())
		}
	}
	reqMap["context.reporter.kind"] = "inbound"
	if downLocalAddress := commonproperties.GetDownstreamLocalAddress(); downLocalAddress != nil {
		fillAddress(reqMap, downLocalAddress, "destination")
	}
	if downDirectRemoteAddress := commonproperties.GetDownstreamDirectRemoteAddress(); downDirectRemoteAddress != nil {
		fillAddress(reqMap, downDirectRemoteAddress, "source")
	}
	if respflags := commonproperties.GetResponseFlags(); respflags != nil {
		reqMap["context.proxy_error_code"] = ParseEnvoyResponseFlags(respflags)
	} else {
		reqMap["context.proxy_error_code"] = "-"
	}

	if tlsproperties := commonproperties.GetTlsProperties(); tlsproperties != nil {
		if localaltname := tlsproperties.GetLocalCertificateProperties().GetSubjectAltName(); localaltname != nil {
			reqMap["destination.principal"] = stripPrincipal(localaltname[0].GetUri())
		}
		if peeraltname := tlsproperties.GetPeerCertificateProperties().GetSubjectAltName(); peeraltname != nil {
			reqMap["source.principal"] = stripPrincipal(peeraltname[0].GetUri())
		}
		reqMap["connection.requested_server_name"] = tlsproperties.GetTlsSniHostname()
	}

	// This is for identifying the log type as Service Access Logs instead of Mixer Report
	// This is more specifically for making migration easier.
	// In migration users will enable access log service and then after will
	// disable Mixer Report. In the time period when both are reporting logs,
	// this field allows users to distinguish between the two types of logs.
	reqMap["context.reporter.uid"] = "envoy_accesslog_service"

	pb.upstreamCluster = commonproperties.GetUpstreamCluster()
	pb.reqMap = reqMap
	scope.Debugf("Returning bag with attributes:\n%v", pb)
	return pb
}

//fills in destination.service.name and destination.service.host after the initial bag has been built
func (pb *EnvoyProtoBag) AddNamespaceDependentAttributes(destinationNamespace string) {
	parts := strings.Split(pb.upstreamCluster, "|")
	var host string
	if len(parts) == 4 {
		host = parts[3]
	} else {
		host = pb.reqMap["request.host"].(string)
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
	namespacePos := strings.IndexAny(host[namePos+1:], ".:")
	var serviceNamespace string
	if namespacePos == -1 {
		serviceNamespace = host[namePos:]
	} else {
		serviceNamespace = host[namePos+1 : namespacePos+namePos+1]
	}
	if serviceNamespace == destinationNamespace {
		pb.reqMap["destination.service.name"] = host[0:namePos]
	} else {
		pb.reqMap["destination.service.name"] = host
	}
	pb.reqMap["destination.service.namespace"] = serviceNamespace
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
func (pb *EnvoyProtoBag) GetReferencedAttributes(globalDict map[string]int32, globalWordCount int) *mixerpb.ReferencedAttributes {
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
