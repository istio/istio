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

package pilot

import (
	"io"

	"istio.io/istio/pkg/test/framework/resource"
)

var (
	_ Instance  = &kubeComponent{}
	_ io.Closer = &kubeComponent{}
)

// TODO remove the pilot component entirely, as it does not really do anything anymore
func newKube(ctx resource.Context, cfg Config) (Instance, error) {
	c := &kubeComponent{
		cluster: ctx.Clusters().GetOrDefault(cfg.Cluster),
	}
	c.id = ctx.TrackResource(c)

	return c, nil
}

type kubeComponent struct {
	id resource.ID

	cluster resource.Cluster
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

// Close stops the kube pilot server.
func (c *kubeComponent) Close() (err error) {
	return
}
