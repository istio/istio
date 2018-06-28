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

package resource

import (
	"reflect"

	prgogo "github.com/gogo/protobuf/proto"
	prlang "github.com/golang/protobuf/proto"
)

// getProtoMessageType returns the Go lang type of the proto with the specified name.
func getProtoMessageType(protoMessageName string, isGogo bool) reflect.Type {
	var t reflect.Type

	if isGogo {
		t = prgogo.MessageType(protoMessageName)
	} else {
		t = prlang.MessageType(protoMessageName)
	}

	if t == nil {
		return nil
	}

	return t.Elem()
}
