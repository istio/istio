// Copyright 2019 Istio Authors
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

package constraint

import (
	"encoding/json"
	"fmt"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/pkg/test/util/structpath"
	"istio.io/istio/pkg/test/util/tmpl"
)

// SelectOp is the operation that needs to be performed.
type SelectOp string

const (
	// SelectGroup indicates that this select is a grouping construct for its children
	SelectGroup SelectOp = "group"

	// SelectEquals applies the equals operation
	SelectEquals SelectOp = "equals"

	// SelectExists applies the exists operation
	SelectExists SelectOp = "exists"

	// SelectNotExists applies the not exists operation
	SelectNotExists SelectOp = "notExists"
)

// Select using structpath and apply specified operation.
type Select struct {
	Expression string
	Op         SelectOp
	Arg        interface{}

	Children []*Select
	Parent   *Select
}

var _ json.Unmarshaler = &Select{}
var _ Check = &Select{}

// UnmarshalJSON implements json.Unmarshaler
func (s *Select) UnmarshalJSON(b []byte) error {
	i := struct {
		Expression string      `json:"select"`
		Exists     *bool       `json:"exists"`
		Equals     interface{} `json:"equals"`
		Then       []*Select   `json:"then"`
	}{}

	if err := json.Unmarshal(b, &i); err != nil {
		return err
	}

	if i.Exists != nil && i.Equals != nil {
		return fmt.Errorf("at most one op(exists/equals) can be specified")
	}

	if i.Exists == nil && i.Equals == nil && i.Then == nil {
		return fmt.Errorf("select clause with no children or operation")
	}

	s.Expression = i.Expression
	s.Op = SelectGroup

	if i.Exists != nil {
		if *i.Exists {
			s.Op = SelectExists
		} else {
			s.Op = SelectNotExists
		}
	}

	if i.Equals != nil {
		s.Op = SelectEquals
		s.Arg = i.Equals
	}

	s.Children = i.Then
	for _, c := range s.Children {
		c.Parent = s
	}

	return nil
}

// ValidateItem implements Check
func (s *Select) ValidateItem(i interface{}, p Params) error {
	pro, ok := i.(proto.Message)
	if !ok {
		return fmt.Errorf("item is not proto: %v", i)
	}

	return s.validate(pro, p)
}

func (s *Select) validate(pro proto.Message, p Params) error {
	instance := s.generateParentInstance(pro, p)

	exp, err := tmpl.Evaluate(s.Expression, p)
	if err != nil {
		return err
	}

	switch s.Op {
	case SelectExists:
		if err := instance.Exists(exp).Check(); err != nil {
			return err
		}
	case SelectNotExists:
		if err := instance.NotExists(exp).Check(); err != nil {
			return err
		}
	case SelectEquals:
		// TODO: Implement instance based equals.
		panic("NYI: SelectEquals")

	case SelectGroup:
		// do nothing

	default:
		return fmt.Errorf("unrecognized select operation: %v", s.Op)
	}

	for _, c := range s.Children {
		if err := c.validate(pro, p); err != nil {
			return err
		}
	}

	return nil
}

func (s *Select) generateParentInstance(pro proto.Message, p Params) *structpath.Instance {
	if s.Parent == nil {
		return structpath.ForProto(pro)
	}

	return s.Parent.generateParentInstance(pro, p)
}
