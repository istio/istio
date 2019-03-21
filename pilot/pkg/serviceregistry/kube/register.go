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

package kube

import (
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/log"
)

var (
	// For most common ports allow the protocol to be guessed, this isn't meant
	// to replace /etc/services. Fully qualified proto[-extra]:port is the
	// recommended usage.
	portsToName = map[int32]string{
		80:   "http",
		443:  "https",
		3306: "mysql",
		8080: "http",
	}
)

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

// RegisterEndpoint registers the endpoint (and the service if it doesn't
// already exists). It creates or updates as needed. When creating it adds the
// optional labels.
func RegisterEndpoint(client kubernetes.Interface, namespace string, svcName string,
	ip string, portsList []NamedPort, labels []string, annotations []string) error {
	getOpt := meta_v1.GetOptions{IncludeUninitialized: true}
	_, err := client.CoreV1().Services(namespace).Get(svcName, getOpt)
	if err != nil {
		log.Warnf("Got '%v' looking up svc '%s' in namespace '%s', attempting to create it", err, svcName, namespace)
		svc := v1.Service{}
		svc.Name = svcName
		for _, p := range portsList {
			svc.Spec.Ports = append(svc.Spec.Ports, v1.ServicePort{Name: p.Name, Port: p.Port})
		}
		addLabelsAndAnnotations(&svc.ObjectMeta, labels, annotations)
		_, err = client.CoreV1().Services(namespace).Create(&svc)
		if err != nil {
			log.Errora("Unable to create service: ", err)
			return err
		}
	}
	eps, err := client.CoreV1().Endpoints(namespace).Get(svcName, getOpt)
	if err != nil {
		log.Warnf("Got '%v' looking up endpoints for '%s' in namespace '%s', attempting to create them",
			err, svcName, namespace)
		endP := v1.Endpoints{}
		endP.Name = svcName // same but does it need to be
		addLabelsAndAnnotations(&endP.ObjectMeta, labels, annotations)
		eps, err = client.CoreV1().Endpoints(namespace).Create(&endP)
		if err != nil {
			log.Errora("Unable to create endpoint: ", err)
			return err
		}
	}
	// To check equality:
	portsMap := make(map[int32]bool, len(portsList))
	for _, e := range portsList {
		portsMap[e.Port] = true
	}

	log.Infof("Before: found endpoints %+v", eps)
	matchingSubset := 0
	for i, ss := range eps.Subsets {
		log.Infof("On ports %+v", ss.Ports)
		for _, ip := range ss.Addresses {
			log.Infof("Found %+v", ip)
		}
		if samePorts(ss.Ports, portsMap) {
			matchingSubset++
			log.Infof("Found matching ports list in existing subset %v", ss.Ports)
			if matchingSubset != 1 {
				log.Errorf("Unexpected match in %d subsets", matchingSubset)
			}
			eps.Subsets[i].Addresses = append(ss.Addresses, v1.EndpointAddress{IP: ip})
		}
	}
	if matchingSubset == 0 {
		newSubSet := v1.EndpointSubset{}
		newSubSet.Addresses = []v1.EndpointAddress{
			{IP: ip},
		}
		for _, p := range portsList {
			newSubSet.Ports = append(newSubSet.Ports, v1.EndpointPort{Name: p.Name, Port: p.Port})
		}
		eps.Subsets = append(eps.Subsets, newSubSet)
		log.Infof("No pre existing exact matching ports list found, created new subset %v", newSubSet)
	}
	eps, err = client.CoreV1().Endpoints(namespace).Update(eps)
	if err != nil {
		log.Errora("Update failed with: ", err)
		return err
	}
	total := 0
	for _, ss := range eps.Subsets {
		total += len(ss.Ports) * len(ss.Addresses)
	}
	log.Infof("Successfully updated %s, now with %d endpoints", eps.Name, total)
	log.Infof("Details: %v", eps)
	return nil
}
