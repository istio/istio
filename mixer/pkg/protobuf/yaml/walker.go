// Copyright 2018 Istio Authors.
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

package yaml

import (
	"fmt"
	"strings"

	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
)

// Visitor defines the callback made by the Walker.
type Visitor interface {
	Visit(name string, val interface{}, field *descriptor.FieldDescriptorProto) (visitor Visitor,
		continueWalk bool, err error)
}

// Walker visits all the nodes inside yaml in a BFS, and calls back with the observed information.
type Walker interface {
	// Walk crawls through the yaml and executes the Visitor callback
	Walk(data map[interface{}]interface{}, msgName string, skipUnknown bool, handler Visitor) error
}

type walker struct {
	resolver Resolver
}

// NewWalker returns a walker that parses yaml
func NewWalker(resolver Resolver) Walker {
	return &walker{resolver: resolver}
}

func (r *walker) Walk(data map[interface{}]interface{}, msgName string, skipUnknown bool, visitor Visitor) error {
	message := r.resolver.ResolveMessage(msgName)
	if message == nil {
		return fmt.Errorf("cannot resolve message '%s'", msgName)
	}
	for k, v := range data {
		fd := findFieldByName(message, k.(string))
		if fd == nil {
			if skipUnknown {
				continue
			}
			return fmt.Errorf("field '%s' not found in message '%s'", k, message.GetName())
		}

		if err := r.walk(k.(string), v, fd, skipUnknown, visitor); err != nil {
			return err
		}

	}
	return nil
}

func (r *walker) walk(name string, data interface{}, field *descriptor.FieldDescriptorProto, skipUnknown bool,
	visitor Visitor) error {
	switch *field.Type {
	case descriptor.FieldDescriptorProto_TYPE_STRING,
		descriptor.FieldDescriptorProto_TYPE_DOUBLE,
		descriptor.FieldDescriptorProto_TYPE_INT64,
		descriptor.FieldDescriptorProto_TYPE_BOOL,
		descriptor.FieldDescriptorProto_TYPE_ENUM:
		_, _, err := visitor.Visit(name, data, field)
		return err
	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		msgData, ok := data.(map[interface{}]interface{})
		if !ok {
			return badTypeError(name, strings.TrimPrefix(field.GetTypeName(), "."), data)
		}

		child, contWalk, err := visitor.Visit(name, data, field)
		if err != nil {
			return err
		}
		if contWalk {
			return r.Walk(msgData, field.GetTypeName(), skipUnknown, child)
		}
		return nil
	}

	return fmt.Errorf("unrecognized field type '%s'", (*field.Type).String())
}
