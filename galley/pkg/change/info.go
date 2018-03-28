//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

// Package change contains data structures for capturing resource state change related information.
package change

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Info captures information about a Kubernetes resource change.
type Info struct {
	Type         Type
	Name         string
	GroupVersion schema.GroupVersion
}

func (i Info) String() string {
	var t string
	switch i.Type {
	case Add:
		t = "Add"
	case Update:
		t = "Update"
	case Delete:
		t = "Delete"
	default:
		t = "Unknown"
	}
	return fmt.Sprintf("Info[Type:%s, Name:%s, GroupVersion:%v]", t, i.Name, i.GroupVersion)
}
