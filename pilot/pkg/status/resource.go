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
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("status",
	"status controller for istio")

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
	found, _ := collections.All.FindByGroupVersionResource(r.GroupVersionResource)
	return config.Key(
		found.Group(), found.Version(), found.Kind(),
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
	gvr, ok := gvk.ToGVR(c.GroupVersionKind)
	if !ok {
		return Resource{}
	}
	return Resource{
		GroupVersionResource: gvr,
		Namespace:            c.Namespace,
		Name:                 c.Name,
		Generation:           strconv.FormatInt(c.Generation, 10),
	}
}

func GetTypedStatus(in any) (out *v1alpha1.IstioStatus, err error) {
	if ret, ok := in.(*v1alpha1.IstioStatus); ok {
		return ret, nil
	}
	return nil, fmt.Errorf("cannot cast %T: %v to IstioStatus", in, in)
}

func GetOGProvider(in any) (out GenerationProvider, err error) {
	if ret, ok := in.(*v1alpha1.IstioStatus); ok && ret != nil {
		return &IstioGenerationProvider{ret}, nil
	}
	return nil, fmt.Errorf("cannot cast %T: %v to GenerationProvider", in, in)
}

func NewIstioContext(stop <-chan struct{}) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-stop
		cancel()
	}()
	return ctx
}
