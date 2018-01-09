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
	"strings"

	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// VerifyEndpoint verifies if the provided IP to deregister has already
// been registered.
func VerifyEndpoint(eps *v1.Endpoints, ip string) bool {
	var match bool
	subsetLen := len(eps.Subsets)
	/*
		1. Check if this is the only entry; if this is the only entry
		   then delete the entry and should return an empty subset.
		2. Check if service endpoint is part of only one subset. If so
		   wipe off the empty subset and return non-empty subsets.
		3. Check if service endpoint is part of multiple subset. If so
		   wipe off the empty subset and coalease non-empty subsets.
	*/
	for i, ss := range eps.Subsets {
		for j, ipaddr := range ss.Addresses {
			if strings.Compare(ipaddr.IP, ip) == 0 {
				/*
					If the IP address match then remove that
					specific IP address.
				*/
				match = true
				if j < len(ss.Addresses) {
					ss.Addresses = append(ss.Addresses[:j], ss.Addresses[j+1:]...)
				} else {
					ss.Addresses = ss.Addresses[:j]
				}
			}
		}
		if len(ss.Addresses) == 0 {
			if len(eps.Subsets) < subsetLen {
				eps.Subsets = append(eps.Subsets[:len(eps.Subsets)], eps.Subsets[len(eps.Subsets)+1:]...)
			} else {
				eps.Subsets = append(eps.Subsets[:i], eps.Subsets[i+1:]...)
			}
		} else {
			if len(eps.Subsets) < subsetLen {
				eps.Subsets[len(eps.Subsets)-1].Addresses = ss.Addresses
			} else {
				eps.Subsets[i].Addresses = ss.Addresses
			}
		}
	}
	return match
}

// DeRegisterEndpoint registers the endpoint (and the service if it
// already exists). It creates or updates as needed.
func DeRegisterEndpoint(client kubernetes.Interface, namespace string, svcName string,
	ip string) error {
	getOpt := meta_v1.GetOptions{IncludeUninitialized: true}
	var match bool
	eps, err := client.CoreV1().Endpoints(namespace).Get(svcName, getOpt)
	if err != nil {
		glog.Error("Endpoint not found for service ", svcName)
		return err
	}
	match = VerifyEndpoint(eps, ip)
	if match == false {
		/*
			If the service endpoint has not been registered
			before, report proper error message.
		*/
		glog.Error("Please make sure the endpoint is registered! Register endpoint using istioctl register")
		return err
	}
	eps, err = client.CoreV1().Endpoints(namespace).Update(eps)
	if err != nil {
		glog.Error("Update failed with: ", err)
		return err
	}
	glog.Error("Endpoint updated %v", eps)
	glog.Error("Deregistered service successfully")
	return nil
}
