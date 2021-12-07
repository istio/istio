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

package kube

import (
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// For most common ports allow the protocol to be guessed, this isn't meant
// to replace /etc/services. Fully qualified proto[-extra]:port is the
// recommended usage.
var portsToName = map[int32]string{
	80:   "http",
	443:  "https",
	3306: "mysql",
	8080: "http",
}

// NamedPort defines the Port and Name tuple needed for services and endpoints.
type NamedPort struct {
	Port int32
	Name string
}

// Str2NamedPort parses a proto:port string into a namePort struct.
func Str2NamedPort(str string) (NamedPort, error) {
	var r NamedPort
	idx := strings.Index(str, ":")
	if idx >= 0 {
		r.Name = str[:idx]
		str = str[idx+1:]
	}
	p, err := strconv.Atoi(str)
	if err != nil {
		return r, err
	}
	r.Port = int32(p)
	if len(r.Name) == 0 {
		name, found := portsToName[r.Port]
		r.Name = name
		if !found {
			r.Name = str
		}
	}
	return r, nil
}

// samePorts returns true if the numerical part of the ports is the same.
// The arrays aren't necessarily sorted so we (re)use a map.
func samePorts(ep []v1.EndpointPort, portsMap map[int32]bool) bool {
	if len(ep) != len(portsMap) {
		return false
	}
	for _, e := range ep {
		if !portsMap[e.Port] {
			return false
		}
	}
	return true
}

// splitEqual splits key=value string into key,value. if no = is found
// the whole string is the key and value is empty.
func splitEqual(str string) (string, string) {
	idx := strings.Index(str, "=")
	var k string
	var v string
	if idx >= 0 {
		k = str[:idx]
		v = str[idx+1:]
	} else {
		k = str
	}
	return k, v
}

// addLabelsAndAnnotations adds labels and annotations to an object.
func addLabelsAndAnnotations(obj *meta_v1.ObjectMeta, labels []string, annotations []string) {
	if obj.Labels == nil {
		obj.Labels = make(map[string]string, len(labels))
	}
	for _, l := range labels {
		k, v := splitEqual(l)
		obj.Labels[k] = v
	}
	if obj.Annotations == nil {
		obj.Annotations = make(map[string]string, len(annotations))
	}
	for _, a := range annotations {
		k, v := splitEqual(a)
		obj.Annotations[k] = v
	}
}
