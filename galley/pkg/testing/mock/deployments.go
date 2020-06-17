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

package mock

import (
	"context"
	"fmt"
	"sync"

	v1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	appsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
)

var _ appsv1.DeploymentInterface = &deploymentImpl{}

type deploymentImpl struct {
	mux         sync.Mutex
	deployments map[string]*v1.Deployment
	watches     Watches
}

func newAppsInterface() appsv1.DeploymentInterface {
	return &deploymentImpl{
		deployments: make(map[string]*v1.Deployment),
	}
}

func (d *deploymentImpl) Create(ctx context.Context, obj *v1.Deployment, opts metav1.CreateOptions) (*v1.Deployment, error) {
	d.mux.Lock()
	defer d.mux.Unlock()

	d.deployments[obj.Name] = obj

	d.watches.Send(watch.Event{
		Type:   watch.Added,
		Object: obj,
	})
	return obj, nil
}

func (d *deploymentImpl) Update(ctx context.Context, obj *v1.Deployment, opts metav1.UpdateOptions) (*v1.Deployment, error) {
	d.mux.Lock()
	defer d.mux.Unlock()

	d.deployments[obj.Name] = obj

	d.watches.Send(watch.Event{
		Type:   watch.Modified,
		Object: obj,
	})
	return obj, nil
}

func (d *deploymentImpl) UpdateStatus(context.Context, *v1.Deployment, metav1.UpdateOptions) (*v1.Deployment, error) {
	panic("not implemented")
}

func (d *deploymentImpl) Delete(ctx context.Context, name string, options metav1.DeleteOptions) error {
	d.mux.Lock()
	defer d.mux.Unlock()

	obj := d.deployments[name]
	if obj == nil {
		return fmt.Errorf("unable to delete deployment %s", name)
	}

	delete(d.deployments, name)

	d.watches.Send(watch.Event{
		Type:   watch.Deleted,
		Object: obj,
	})
	return nil
}

func (d *deploymentImpl) DeleteCollection(ctx context.Context, options metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	panic("not implemented")
}

func (d *deploymentImpl) Get(ctx context.Context, name string, options metav1.GetOptions) (*v1.Deployment, error) {
	panic("not implemented")
}

func (d *deploymentImpl) List(ctx context.Context, opts metav1.ListOptions) (*v1.DeploymentList, error) {
	d.mux.Lock()
	defer d.mux.Unlock()

	out := &v1.DeploymentList{}

	for _, v := range d.deployments {
		out.Items = append(out.Items, *v)
	}

	return out, nil
}

func (d *deploymentImpl) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	d.mux.Lock()
	defer d.mux.Unlock()

	w := NewWatch()
	d.watches = append(d.watches, w)

	// Send add events for all current resources.
	for _, d := range d.deployments {
		w.Send(watch.Event{
			Type:   watch.Added,
			Object: d,
		})
	}

	return w, nil
}

func (d *deploymentImpl) Patch(ctx context.Context, name string, pt types.PatchType,
	data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.Deployment, err error) {
	panic("not implemented")
}

func (d *deploymentImpl) GetScale(ctx context.Context, deploymentName string, options metav1.GetOptions) (*autoscalingv1.Scale, error) {
	panic("not implemented")
}

func (d *deploymentImpl) UpdateScale(ctx context.Context, deploymentName string,
	scale *autoscalingv1.Scale, opts metav1.UpdateOptions) (*autoscalingv1.Scale, error) {
	panic("not implemented")
}
