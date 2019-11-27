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

package v1alpha2

import (
	"io/ioutil"
	"reflect"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/types"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"istio.io/operator/pkg/object"
	"istio.io/operator/pkg/util"
)

func TestDeepCopy(t *testing.T) {
	cases := []struct {
		name      string
		createICP func(t *testing.T) *IstioControlPlane
	}{
		{
			name: "with-metadata",
			createICP: func(t *testing.T) *IstioControlPlane {
				now := meta.NewTime(time.Now().Truncate(time.Second))
				return &IstioControlPlane{
					ObjectMeta: meta.ObjectMeta{
						Name:                       "name",
						GenerateName:               "generateName",
						Namespace:                  "namespace",
						SelfLink:                   "selfLink",
						UID:                        "uid",
						ResourceVersion:            "resourceVersion",
						Generation:                 1,
						CreationTimestamp:          now,
						DeletionTimestamp:          &now,
						DeletionGracePeriodSeconds: pointer.Int64Ptr(15),
						Labels: map[string]string{
							"label": "value",
						},
						Annotations: map[string]string{
							"annotation": "value",
						},
						OwnerReferences: []meta.OwnerReference{
							{
								APIVersion:         "v1",
								Kind:               "Foo",
								Name:               "foo",
								UID:                "123",
								Controller:         pointer.BoolPtr(true),
								BlockOwnerDeletion: pointer.BoolPtr(true),
							},
						},
						Finalizers:  []string{"finalizer"},
						ClusterName: "cluster",
					},
					Spec: &IstioControlPlaneSpec{
						Cni: &CNIFeatureSpec{
							Enabled: &BoolValueForPB{types.BoolValue{Value: true}},
						},
						Profile: "profile",
						Hub:     "hub",
						Tag:     "tag",
					},
				}
			},
		},
		{
			name: "default-profile",
			createICP: func(t *testing.T) *IstioControlPlane {
				icp, err := readICPFromYAMLFile("../../../../data/profiles/default.yaml")
				if err != nil {
					t.Fatalf("Could not read ICP from YAML file: %v", err)
				}
				return icp
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			icp := tc.createICP(t)
			icp2 := icp.DeepCopy()

			if !reflect.DeepEqual(icp, icp2) {
				t.Fatalf("Expected IstioControlPlanes to be equal, but they weren't.\n"+
					"  Expected: %+v,\n"+
					"       got: %+v", *icp, *icp2)
			}
		})
	}
}

func readICPFromYAMLFile(filename string) (*IstioControlPlane, error) {
	yml, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	o, err := object.ParseYAMLToK8sObject(yml)
	if err != nil {
		return nil, err
	}
	y, err := yaml.Marshal(o.UnstructuredObject().Object["spec"])
	if err != nil {
		return nil, err
	}

	// UnmarshalWithJSONPB fails when reading ICP.metadata, so we only parse the spec
	spec := &IstioControlPlaneSpec{}
	if err := util.UnmarshalWithJSONPB(string(y), spec); err != nil {
		return nil, err
	}
	return &IstioControlPlane{
		Spec: spec,
	}, nil
}
