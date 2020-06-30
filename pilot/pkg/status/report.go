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

package status

import (
	"strings"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"

	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type DistributionReport struct {
	Reporter            string         `json:"reporter"`
	DataPlaneCount      int            `json:"dataPlaneCount"`
	InProgressResources map[string]int `json:"inProgressResources"`
}

func ReportFromYaml(content []byte) (DistributionReport, error) {
	out := DistributionReport{}
	err := yaml.Unmarshal(content, &out)
	return out, err
}

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
		Namespace:       pieces[3],
		Name:            pieces[4],
		ResourceVersion: pieces[5],
	}
}

// TODO: maybe replace with a kubernetes resource identifier, if that's a thing
type Resource struct {
	schema.GroupVersionResource
	Namespace       string
	Name            string
	ResourceVersion string
}

func (r Resource) String() string {
	return strings.Join([]string{r.Group, r.Version, r.Resource, r.Namespace, r.Name, r.ResourceVersion}, "/")
}

func (r *Resource) ToModelKey() string {
	// we have a resource here, but model keys use kind.  Use the schema to find the correct kind.
	found, _ := collections.All.FindByPlural(r.Group, r.Version, r.Resource)
	return model.Key(found.Resource().Kind(), r.Name, r.Namespace)
}

func ResourceFromModelConfig(c model.Config) *Resource {
	gvr := GVKtoGVR(c.GroupVersionKind)
	if gvr == nil {
		return nil
	}
	return &Resource{
		GroupVersionResource: *gvr,
		Namespace:            c.Namespace,
		Name:                 c.Name,
		ResourceVersion:      c.ResourceVersion,
	}
}

func GVKtoGVR(in resource.GroupVersionKind) *schema.GroupVersionResource {
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
