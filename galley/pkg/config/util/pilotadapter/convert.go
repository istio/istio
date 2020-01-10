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

package pilotadapter

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/istio/galley/pkg/config/schema/collection"
	"istio.io/istio/galley/pkg/config/schema/resource"
	"istio.io/istio/galley/pkg/config/util/pb"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/util/gogoprotomarshal"
)

// TODO(nmittler): Remove this once pilot migrates to the galley schema model.
func ConvertPilotSchemasToGalley(in schema.Set) collection.Schemas {
	b := collection.NewSchemasBuilder()
	for _, i := range in {
		b.MustAdd(ConvertPilotSchemaToGalley(i))
	}
	return b.Build()
}

// TODO(nmittler): Remove this once pilot migrates to the galley schema model.
func ConvertPilotSchemaToGalley(in schema.Instance) collection.Schema {
	return collection.Builder{
		Name:         in.Collection,
		VariableName: in.VariableName,
		Resource: resource.Builder{
			ClusterScoped: in.ClusterScoped,
			Kind:          in.Type,
			Plural:        in.Plural,
			Group:         crd.ResourceGroup(&in), // ensure ends with ".istio.io"
			Version:       in.Version,
			Proto:         in.MessageName,
			ValidateProto: in.Validate,
		}.MustBuild(),
	}.MustBuild()
}

// TODO(nmittler): Remove this once pilot migrates to the galley schema model.
func ConvertGalleySchemasToPilot(in collection.Schemas) schema.Set {
	all := in.All()
	out := make(schema.Set, 0, len(all))
	for _, s := range all {
		// Only add known types.
		if _, ok := crd.KnownTypes[crd.CamelCaseToKebabCase(s.Resource().Kind())]; ok {
			out = append(out, ConvertGalleySchemaToPilot(s))
		}
	}
	return out
}

// TODO(nmittler): Remove this once pilot migrates to the galley schema model.
func ConvertGalleySchemaToPilot(in collection.Schema) schema.Instance {
	return schema.Instance{
		ClusterScoped: in.Resource().IsClusterScoped(),
		VariableName:  in.VariableName(),
		Type:          in.Resource().Kind(),
		Plural:        in.Resource().Plural(),
		Group:         in.Resource().Group(),
		Version:       in.Resource().Version(),
		MessageName:   in.Resource().Proto(),
		Validate:      in.Resource().ValidateProto,
		Collection:    in.Name().String(),
	}
}

// TODO(nmittler): Remove this once pilot migrates to the galley schema model.
func ConvertConfigToObject(schema collection.Schema, cfg model.Config) (crd.IstioObject, error) {
	spec, err := gogoprotomarshal.ToJSONMap(cfg.Spec)
	if err != nil {
		return nil, err
	}
	namespace := cfg.Namespace
	if namespace == "" && !schema.Resource().IsClusterScoped() {
		namespace = metav1.NamespaceDefault
	}

	return &istioObjImpl{
		TypeMeta: metav1.TypeMeta{
			Kind:       schema.Resource().Kind(),
			APIVersion: schema.Resource().APIVersion(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              cfg.Name,
			Namespace:         namespace,
			ResourceVersion:   cfg.ResourceVersion,
			Labels:            cfg.Labels,
			Annotations:       cfg.Annotations,
			CreationTimestamp: metav1.NewTime(cfg.CreationTimestamp),
		},
		Spec: spec,
	}, nil
}

// TODO(nmittler): Remove this once pilot migrates to the galley schema model.
func ConvertObjectToConfig(schema collection.Schema, object crd.IstioObject, domain string) (*model.Config, error) {
	data, err := pb.UnmarshalFromJSONMap(schema, object.GetSpec())
	if err != nil {
		return nil, err
	}
	meta := object.GetObjectMeta()

	return &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              schema.Resource().Kind(),
			Group:             schema.Resource().Group(),
			Version:           schema.Resource().Version(),
			Name:              meta.Name,
			Namespace:         meta.Namespace,
			Domain:            domain,
			Labels:            meta.Labels,
			Annotations:       meta.Annotations,
			ResourceVersion:   meta.ResourceVersion,
			CreationTimestamp: meta.CreationTimestamp.Time,
		},
		Spec: data,
	}, nil
}

var _ crd.IstioObject = &istioObjImpl{}

type istioObjImpl struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              map[string]interface{} `json:"spec"`
}

func (o *istioObjImpl) GetSpec() map[string]interface{} {
	panic("implement me")
}

func (o *istioObjImpl) SetSpec(spec map[string]interface{}) {
	o.Spec = spec
}

func (o *istioObjImpl) GetObjectMeta() metav1.ObjectMeta {
	return o.ObjectMeta
}

func (o *istioObjImpl) SetObjectMeta(metadata metav1.ObjectMeta) {
	o.ObjectMeta = metadata
}

func (o *istioObjImpl) DeepCopyInto(out *istioObjImpl) {
	*out = *o
	out.TypeMeta = o.TypeMeta
	o.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = o.Spec
}

func (o *istioObjImpl) DeepCopyObject() runtime.Object {
	if o == nil {
		return nil
	}
	out := &istioObjImpl{}
	o.DeepCopyInto(out)
	return out
}
