//  Copyright Istio Authors
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

package policy

import (
	"context"
	"time"

	"github.com/gogo/protobuf/proto"
	"google.golang.org/grpc"
)

// Controller is the control interface for the policy backend. The tests can use the interface to control the
// fake.
type Controller struct {
	client ControllerServiceClient
}

// NewController creates and returns a new instance of a controller client for the backend.
func NewController(address string) (*Controller, error) {
	conn, err := grpc.Dial(address, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	c := NewControllerServiceClient(conn)

	return &Controller{
		client: c,
	}, nil
}

// DenyCheck causes the policy backend to deny all check requests when set to true.
func (c *Controller) DenyCheck(deny bool) error {
	s := newSettings().setDenyCheck(deny)
	return c.send(s)
}

// AllowCheck causes the policy backend to allow all check requests with the supplied
// valid duration and valid count in check result.
func (c *Controller) AllowCheck(d time.Duration, count int32) error {
	s := newSettings().setDenyCheck(false)
	if count > 0 {
		s.setValidCount(count)
	}
	if d > 0 {
		s.setValidDuration(d)
	}
	return c.send(s)
}

// GetReports returns the currently accumulated report instances.
func (c *Controller) GetReports() ([]proto.Message, error) {
	request := &GetReportsRequest{}
	response, err := c.client.GetReports(context.TODO(), request)
	if err != nil {
		return nil, err
	}

	result := make([]proto.Message, len(response.Instances))
	for i, inst := range response.Instances {
		p, err := fromAny(inst)
		if err != nil {
			return nil, err
		}
		result[i] = p
	}

	return result, nil
}

// Reset the state of the backend.
func (c *Controller) Reset() error {
	request := &ResetRequest{}
	_, err := c.client.Reset(context.TODO(), request)
	return err
}

func (c *Controller) send(s settings) error {
	var m map[string]string = s

	request := &SetRequest{
		Settings: m,
	}

	_, err := c.client.Set(context.TODO(), request)

	return err
}
