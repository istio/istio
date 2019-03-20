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
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/log"
)

// removeIPFromEndpoint verifies if the provided IP to deregister
// has already been registered.
func removeIPFromEndpoint(eps *v1.Endpoints, ip string) bool {
	var match bool
	/*
		1. Check if this is the only entry; if this is the only entry
		   then delete the entry and should return an empty subset.
		2. Check if service endpoint is part of only one subset. If so
		   wipe off the empty subset and return non-empty subsets.
		3. Check if service endpoint is part of multiple subset. If so
		   wipe off the empty subset and coalease non-empty subsets.
	*/
	for i := 0; i < len(eps.Subsets); {
		ss := eps.Subsets[i]
		for j := 0; j < len(ss.Addresses); {
			ipaddr := ss.Addresses[j]
			if strings.Compare(ipaddr.IP, ip) == 0 {
				/*
					If the IP address match then remove that
					specific IP address.
				*/
				match = true
				ss.Addresses = append(ss.Addresses[:j], ss.Addresses[j+1:]...)
			} else {
				j++
			}
		}
		if len(ss.Addresses) == 0 {
			eps.Subsets = append(eps.Subsets[:i], eps.Subsets[i+1:]...)
		} else {
			eps.Subsets[i].Addresses = ss.Addresses
			i++
		}
	}
	return match
}

// DeRegisterEndpoint removes the endpoint (and the service if it
// already exists) from Kubernetes. It creates or updates as needed.
func DeRegisterEndpoint(client kubernetes.Interface, namespace string, svcName string,
	ip string) error {
	getOpt := meta_v1.GetOptions{IncludeUninitialized: true}
	var match bool
	eps, err := client.CoreV1().Endpoints(namespace).Get(svcName, getOpt)
	if err != nil {
		log.Errora("Endpoint not found for service ", svcName)
		return err
	}
	match = removeIPFromEndpoint(eps, ip)
	if !match {
		/*
			If the service endpoint has not been registered
			before, report proper error message.
		*/
		err = fmt.Errorf("could not find ip %s in svc %s endpoints", ip, svcName)
		log.Errora(err)
		return err
	}
	eps, err = client.CoreV1().Endpoints(namespace).Update(eps)
	if err != nil {
		log.Errora("Update failed with: ", err)
		return err
	}
	log.Infof("Endpoint updated %v", eps)
	log.Infof("Deregistered service successfully")
	return nil
}
