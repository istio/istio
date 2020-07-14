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

package integration

import (
	"io"

	"istio.io/istio/pkg/test/framework/resource"
)

var _ io.Closer = &component{}

type component struct {
	name        string
	id          resource.ID
	handleClose func(*component)
}

func (c *component) ID() resource.ID {
	return c.id
}

func (c *component) Close() error {
	if c.handleClose != nil {
		c.handleClose(c)
	}
	return nil
}

func newComponent(ctx resource.Context, name string, handleClose func(*component)) *component {
	c := &component{
		name:        name,
		handleClose: handleClose,
	}
	c.id = ctx.TrackResource(c)
	return c
}
