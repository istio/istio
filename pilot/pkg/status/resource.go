/*
 Copyright Istio Authors

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package status

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/api/meta/v1alpha1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/pkg/log"
)

var scope = log.RegisterScope("status",
	"status controller for istio", 0)

func ResourceFromString(s string) *Resource {
	pieces := strings.Split(s, "/")
	if len(pieces) != 6 {
		scope.Errorf("cannot unmarshal %s into resource identifier", s)
		return nil
	}
	return &Resource{
		GroupVersionResource: schema.GroupVersionResource{
			Group:    pieces[0],
			Version:  pieces[1],
			Resource: pieces[2],
		},
		Namespace:  pieces[3],
		Name:       pieces[4],
		Generation: pieces[5],
	}
}

// TODO: maybe replace with a kubernetes resource identifier, if that's a thing
type Resource struct {
	schema.GroupVersionResource
	Namespace  string
	Name       string
	Generation string
}

func (r Resource) String() string {
	return strings.Join([]string{r.Group, r.Version, r.GroupVersionResource.Resource, r.Namespace, r.Name, r.Generation}, "/")
}

func (r *Resource) ToModelKey() string {
	// we have a resource here, but model keys use kind.  Use the schema to find the correct kind.
	found, _ := collections.All.FindByPlural(r.Group, r.Version, r.Resource)
	return config.Key(
		found.Resource().Group(), found.Resource().Version(), found.Resource().Kind(),
		r.Name, r.Namespace)
}

func ResourceFromMetadata(i resource.Metadata) Resource {
	return Resource{
		GroupVersionResource: i.Schema.GroupVersionResource(),
		Namespace:            i.FullName.Namespace.String(),
		Name:                 i.FullName.Name.String(),
		Generation:           strconv.FormatInt(i.Generation, 10),
	}
}

func ResourceFromModelConfig(c config.Config) Resource {
	gvr := GVKtoGVR(c.GroupVersionKind)
	if gvr == nil {
		return Resource{}
	}
	return Resource{
		GroupVersionResource: *gvr,
		Namespace:            c.Namespace,
		Name:                 c.Name,
		Generation:           strconv.FormatInt(c.Generation, 10),
	}
}

func ResourceToModelConfig(c Resource) config.Meta {
	gvk := GVRtoGVK(c.GroupVersionResource)
	gen, err := strconv.Atoi(c.Generation)
	if err != nil {
		log.Errorf("failed to convert resource generation %s to int: %s", c.Generation, err)
		return config.Meta{}
	}
	return config.Meta{
		GroupVersionKind: gvk,
		Namespace:        c.Namespace,
		Name:             c.Name,
		Generation:       int64(gen),
	}
}

func GetTypedStatus(in any) (out *v1alpha1.IstioStatus, err error) {
	if ret, ok := in.(*v1alpha1.IstioStatus); ok {
		return ret, nil
	}
	return nil, fmt.Errorf("cannot cast %T: %v to IstioStatus", in, in)
}

func GetOGProvider(in any) (out GenerationProvider, err error) {
	if ret, ok := in.(*v1alpha1.IstioStatus); ok {
		return &IstioGenerationProvider{ret}, nil
	}
	return nil, fmt.Errorf("cannot cast %T: %v to GenerationProvider", in, in)
}

func GVKtoGVR(in config.GroupVersionKind) *schema.GroupVersionResource {
	found, ok := collections.All.FindByGroupVersionKind(in)
	if !ok {
		return nil
	}
	return &schema.GroupVersionResource{
		Group:    in.Group,
		Version:  in.Version,
		Resource: found.Resource().Plural(),
	}
}

func GVRtoGVK(in schema.GroupVersionResource) config.GroupVersionKind {
	found, ok := collections.All.FindByGroupVersionResource(in)
	if !ok {
		return config.GroupVersionKind{}
	}
	return found.Resource().GroupVersionKind()
}

func NewIstioContext(stop <-chan struct{}) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-stop
		cancel()
	}()
	return ctx
}
