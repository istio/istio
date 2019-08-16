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

package native

import (
	"context"
	"io"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/docker"
)

var _ io.Closer = &ImageRegistry{}

type ImageRegistry struct {
	client *client.Client
	images map[string]docker.Image
	mux    sync.Mutex
}

type ImageCreateFunc func(dockerClient *client.Client) (docker.Image, error)

func newImageRegistry(client *client.Client) *ImageRegistry {
	return &ImageRegistry{
		client: client,
		images: make(map[string]docker.Image),
	}
}

func (r *ImageRegistry) GetOrCreate(name string, create ImageCreateFunc) (docker.Image, error) {
	r.mux.Lock()
	defer r.mux.Unlock()

	image := r.images[name]
	if image == "" {
		var err error
		if image, err = create(r.client); err != nil {
			return "", err
		}
		r.images[name] = image
	}

	return image, nil
}

func (r *ImageRegistry) Close() (err error) {
	r.mux.Lock()
	defer r.mux.Unlock()
	for name, image := range r.images {
		_, e := r.client.ImageRemove(context.Background(), image.String(), types.ImageRemoveOptions{
			PruneChildren: true,
		})
		err = multierror.Append(err, e).ErrorOrNil()
		delete(r.images, name)
	}
	return
}
