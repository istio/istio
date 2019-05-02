/*
Copyright The Helm Authors.

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

package driver // import "k8s.io/helm/pkg/storage/driver"

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kblabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	rspb "k8s.io/helm/pkg/proto/hapi/release"
	storageerrors "k8s.io/helm/pkg/storage/errors"
)

var _ Driver = (*Secrets)(nil)

// SecretsDriverName is the string name of the driver.
const SecretsDriverName = "Secret"

// Secrets is a wrapper around an implementation of a kubernetes
// SecretsInterface.
type Secrets struct {
	impl corev1.SecretInterface
	Log  func(string, ...interface{})
}

// NewSecrets initializes a new Secrets wrapping an implementation of
// the kubernetes SecretsInterface.
func NewSecrets(impl corev1.SecretInterface) *Secrets {
	return &Secrets{
		impl: impl,
		Log:  func(_ string, _ ...interface{}) {},
	}
}

// Name returns the name of the driver.
func (secrets *Secrets) Name() string {
	return SecretsDriverName
}

// Get fetches the release named by key. The corresponding release is returned
// or error if not found.
func (secrets *Secrets) Get(key string) (*rspb.Release, error) {
	// fetch the secret holding the release named by key
	obj, err := secrets.impl.Get(key, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, storageerrors.ErrReleaseNotFound(key)
		}

		secrets.Log("get: failed to get %q: %s", key, err)
		return nil, err
	}
	// found the secret, decode the base64 data string
	r, err := decodeRelease(string(obj.Data["release"]))
	if err != nil {
		secrets.Log("get: failed to decode data %q: %s", key, err)
		return nil, err
	}
	// return the release object
	return r, nil
}

// List fetches all releases and returns the list releases such
// that filter(release) == true. An error is returned if the
// secret fails to retrieve the releases.
func (secrets *Secrets) List(filter func(*rspb.Release) bool) ([]*rspb.Release, error) {
	lsel := kblabels.Set{"OWNER": "TILLER"}.AsSelector()
	opts := metav1.ListOptions{LabelSelector: lsel.String()}

	list, err := secrets.impl.List(opts)
	if err != nil {
		secrets.Log("list: failed to list: %s", err)
		return nil, err
	}

	var results []*rspb.Release

	// iterate over the secrets object list
	// and decode each release
	for _, item := range list.Items {
		rls, err := decodeRelease(string(item.Data["release"]))
		if err != nil {
			secrets.Log("list: failed to decode release: %v: %s", item, err)
			continue
		}
		if filter(rls) {
			results = append(results, rls)
		}
	}
	return results, nil
}

// Query fetches all releases that match the provided map of labels.
// An error is returned if the secret fails to retrieve the releases.
func (secrets *Secrets) Query(labels map[string]string) ([]*rspb.Release, error) {
	ls := kblabels.Set{}
	for k, v := range labels {
		if errs := validation.IsValidLabelValue(v); len(errs) != 0 {
			return nil, fmt.Errorf("invalid label value: %q: %s", v, strings.Join(errs, "; "))
		}
		ls[k] = v
	}

	opts := metav1.ListOptions{LabelSelector: ls.AsSelector().String()}

	list, err := secrets.impl.List(opts)
	if err != nil {
		secrets.Log("query: failed to query with labels: %s", err)
		return nil, err
	}

	if len(list.Items) == 0 {
		return nil, storageerrors.ErrReleaseNotFound(labels["NAME"])
	}

	var results []*rspb.Release
	for _, item := range list.Items {
		rls, err := decodeRelease(string(item.Data["release"]))
		if err != nil {
			secrets.Log("query: failed to decode release: %s", err)
			continue
		}
		results = append(results, rls)
	}
	return results, nil
}

// Create creates a new Secret holding the release. If the
// Secret already exists, ErrReleaseExists is returned.
func (secrets *Secrets) Create(key string, rls *rspb.Release) error {
	// set labels for secrets object meta data
	var lbs labels

	lbs.init()
	lbs.set("CREATED_AT", strconv.Itoa(int(time.Now().Unix())))

	// create a new secret to hold the release
	obj, err := newSecretsObject(key, rls, lbs)
	if err != nil {
		secrets.Log("create: failed to encode release %q: %s", rls.Name, err)
		return err
	}
	// push the secret object out into the kubiverse
	if _, err := secrets.impl.Create(obj); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return storageerrors.ErrReleaseExists(rls.Name)
		}

		secrets.Log("create: failed to create: %s", err)
		return err
	}
	return nil
}

// Update updates the Secret holding the release. If not found
// the Secret is created to hold the release.
func (secrets *Secrets) Update(key string, rls *rspb.Release) error {
	// set labels for secrets object meta data
	var lbs labels

	lbs.init()
	lbs.set("MODIFIED_AT", strconv.Itoa(int(time.Now().Unix())))

	// create a new secret object to hold the release
	obj, err := newSecretsObject(key, rls, lbs)
	if err != nil {
		secrets.Log("update: failed to encode release %q: %s", rls.Name, err)
		return err
	}
	// push the secret object out into the kubiverse
	_, err = secrets.impl.Update(obj)
	if err != nil {
		secrets.Log("update: failed to update: %s", err)
		return err
	}
	return nil
}

// Delete deletes the Secret holding the release named by key.
func (secrets *Secrets) Delete(key string) (rls *rspb.Release, err error) {
	// fetch the release to check existence
	if rls, err = secrets.Get(key); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, storageerrors.ErrReleaseExists(rls.Name)
		}

		secrets.Log("delete: failed to get release %q: %s", key, err)
		return nil, err
	}
	// delete the release
	if err = secrets.impl.Delete(key, &metav1.DeleteOptions{}); err != nil {
		return rls, err
	}
	return rls, nil
}

// newSecretsObject constructs a kubernetes Secret object
// to store a release. Each secret data entry is the base64
// encoded string of a release's binary protobuf encoding.
//
// The following labels are used within each secret:
//
//    "MODIFIED_AT"    - timestamp indicating when this secret was last modified. (set in Update)
//    "CREATED_AT"     - timestamp indicating when this secret was created. (set in Create)
//    "VERSION"        - version of the release.
//    "STATUS"         - status of the release (see proto/hapi/release.status.pb.go for variants)
//    "OWNER"          - owner of the secret, currently "TILLER".
//    "NAME"           - name of the release.
//
func newSecretsObject(key string, rls *rspb.Release, lbs labels) (*v1.Secret, error) {
	const owner = "TILLER"

	// encode the release
	s, err := encodeRelease(rls)
	if err != nil {
		return nil, err
	}

	if lbs == nil {
		lbs.init()
	}

	// apply labels
	lbs.set("NAME", rls.Name)
	lbs.set("OWNER", owner)
	lbs.set("STATUS", rspb.Status_Code_name[int32(rls.Info.Status.Code)])
	lbs.set("VERSION", strconv.Itoa(int(rls.Version)))

	// create and return secret object
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   key,
			Labels: lbs.toMap(),
		},
		Data: map[string][]byte{"release": []byte(s)},
	}, nil
}
