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

package rt

import (
	"github.com/gogo/protobuf/proto"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	resource2 "istio.io/istio/pkg/config/schema/resource"
)

// ToResource converts the given object and proto to a resource.Instance
func ToResource(object metav1.Object, schema collection.Schema, item proto.Message, source resource.Reference) *resource.Instance {
	var o *Origin

	name := resource.NewFullName(resource.Namespace(object.GetNamespace()), resource.LocalName(object.GetName()))
	version := resource.Version(object.GetResourceVersion())

	var resourceSchema resource2.Schema
	if schema != nil {
		resourceSchema = schema.Resource()
		o = &Origin{
			FullName:   name,
			Collection: schema.Name(),
			Kind:       schema.Resource().Kind(),
			Version:    version,
			Ref:        source,
		}
	}

	return &resource.Instance{
		Metadata: resource.Metadata{
			Schema:      resourceSchema,
			FullName:    name,
			Version:     version,
			Annotations: object.GetAnnotations(),
			Labels:      object.GetLabels(),
			CreateTime:  object.GetCreationTimestamp().Time,
		},
		Message: item,
		Origin:  o,
	}
}
