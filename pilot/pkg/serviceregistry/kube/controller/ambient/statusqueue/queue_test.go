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

package statusqueue_test

import (
	"fmt"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient/statusqueue"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

type serviceStatus struct {
	Target     model.TypedObject
	Conditions model.ConditionSet
}

func (s serviceStatus) GetStatusTarget() model.TypedObject {
	return s.Target
}

func (s serviceStatus) ResourceName() string {
	return s.Target.String()
}

func (s serviceStatus) GetConditions() model.ConditionSet {
	return s.Conditions
}

func TestQueue(t *testing.T) {
	q := statusqueue.NewQueue()
	c := kube.NewFakeClient()
	svc := kclient.New[*v1.Service](c)
	svcs := krt.WrapClient[*v1.Service](svc)
	col := krt.NewCollection(svcs, func(ctx krt.HandlerContext, i *v1.Service) *serviceStatus {
		conds := model.ConditionSet{
			model.ConditionType("t1"): nil,
			model.ConditionType("t2"): nil,
			model.ConditionType("t3"): nil,
		}
		for _, set := range strings.Split(i.Annotations["conditions"], ",") {
			k, v, _ := strings.Cut(set, "=")
			conds[model.ConditionType(k)] = &model.Condition{Status: v == "true", Reason: "some reason"}
		}
		return &serviceStatus{
			Target: model.TypedObject{
				NamespacedName: config.NamespacedName(i),
				Kind:           kind.Service,
			},
			Conditions: conds,
		}
	})
	statusqueue.Register(q, "services", col, func(status serviceStatus) (kclient.Patcher, []string) {
		return kclient.ToPatcher(svc), nil
	})
	clienttest.Wrap(t, svc).Create(&v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "none", Namespace: "default"}})
	clienttest.Wrap(t, svc).Create(&v1.Service{ObjectMeta: metav1.ObjectMeta{
		Name:        "conds",
		Namespace:   "default",
		Annotations: map[string]string{"conditions": "t1=true"},
	}})
	stop := test.NewStop(t)
	c.RunAndWait(stop)
	go q.Run(stop)

	expectConditions := func(name string, conds map[string]bool) {
		t.Helper()
		retry.UntilSuccessOrFail(t, func() error {
			s := svc.Get(name, "default")
			allHave := slices.Map(s.Status.Conditions, func(t metav1.Condition) string {
				return t.Type
			})
			have := slices.GroupUnique(s.Status.Conditions, func(t metav1.Condition) string {
				return t.Type
			})

			for wc, wv := range conds {
				realCond, f := have[wc]
				if !f {
					return fmt.Errorf("expected condition %q, had %v", wc, allHave)
				}
				delete(have, wc)
				if (realCond.Status == metav1.ConditionTrue) != wv {
					return fmt.Errorf("expected condition %q to be %v, got %v", wc, realCond.Status, wv)
				}
			}
			if len(have) > 0 {
				return fmt.Errorf("unexpected conditions, wanted %v, got %v", maps.Keys(conds), allHave)
			}
			return nil
		})
	}
	expectConditions("none", nil)
	expectConditions("conds", map[string]bool{"t1": true})

	clienttest.Wrap(t, svc).CreateOrUpdate(&v1.Service{ObjectMeta: metav1.ObjectMeta{
		Name:        "conds",
		Namespace:   "default",
		Annotations: map[string]string{"conditions": "t2=true"},
	}})
	expectConditions("conds", map[string]bool{"t2": true})
}
