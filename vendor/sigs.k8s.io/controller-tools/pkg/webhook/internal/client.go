/*
Copyright 2018 The Kubernetes Authors.

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

package internal

import (
	"bytes"
	"context"
	"errors"
	"sort"

	"github.com/ghodss/yaml"
	"github.com/spf13/afero"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var decoder = scheme.Codecs.UniversalDeserializer()

// NewManifestClient constructs a new manifestClient.
func NewManifestClient(file string) client.Client {
	return &manifestClient{
		ManifestFile: file,
		fs:           afero.NewOsFs(),
	}
}

// manifestClient reads from and writes to the file specified by ManifestFile.
type manifestClient struct {
	ManifestFile string

	objects map[schema.GroupVersionKind][]byte
	fs      afero.Fs
}

var _ client.Client = &manifestClient{}

func (c *manifestClient) index() error {
	c.objects = map[schema.GroupVersionKind][]byte{}

	_, err := c.fs.Stat(c.ManifestFile)
	if err != nil {
		return nil
	}

	b, err := afero.ReadFile(c.fs, c.ManifestFile)
	if err != nil {
		return err
	}
	objs := bytes.Split(b, []byte("---\n"))
	for _, objectB := range objs {
		objB := bytes.TrimSpace(objectB)
		if len(objB) == 0 {
			continue
		}
		_, gvk, err := decoder.Decode(objB, nil, nil)
		if err != nil {
			return err
		}
		c.objects[*gvk] = objectB
	}
	return nil
}

// Get read from the target file.
func (c *manifestClient) Get(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
	if obj == nil {
		return errors.New("obj should not be nil")
	}
	err := c.index()
	if err != nil {
		return err
	}
	gvk := obj.GetObjectKind().GroupVersionKind()
	objectB, found := c.objects[gvk]
	if !found {
		return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
	}
	_, _, err = decoder.Decode(objectB, nil, obj)
	return err
}

// List does nothing, it should not be invoked.
func (c *manifestClient) List(ctx context.Context, opts *client.ListOptions, list runtime.Object) error {
	return errors.New("method List is not implemented")
}

// Create creates an object and write it to the target file with other objects if any.
// If the object needs to be written already exists, it will error out.
func (c *manifestClient) Create(ctx context.Context, obj runtime.Object) error {
	if obj == nil {
		return errors.New("obj should not be nil")
	}
	err := c.index()
	if err != nil {
		return err
	}

	gvk := obj.GetObjectKind().GroupVersionKind()
	_, found := c.objects[gvk]
	if found {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return err
		}
		return apierrors.NewAlreadyExists(schema.GroupResource{}, accessor.GetName())
	}
	b, err := yaml.Marshal(obj)
	if err != nil {
		return err
	}
	c.objects[gvk] = b
	return c.writeObjects()
}

// Delete does nothing, it should not be invoked.
func (c *manifestClient) Delete(ctx context.Context, obj runtime.Object, opts ...client.DeleteOptionFunc) error {
	return errors.New("method Delete is not implemented")
}

// Update replace the object if it already exists on the target file.
// Otherwise, it creates the object.
func (c *manifestClient) Update(ctx context.Context, obj runtime.Object) error {
	if obj == nil {
		return errors.New("obj should not be nil")
	}
	err := c.index()
	if err != nil {
		return err
	}

	gvk := obj.GetObjectKind().GroupVersionKind()
	m, err := yaml.Marshal(obj)
	if err != nil {
		return err
	}
	c.objects[gvk] = m
	return c.writeObjects()
}

// writeObjects writes objects to the target file in yaml format separated by `---`.
func (c *manifestClient) writeObjects() error {
	needSeparator := false
	buf := bytes.NewBuffer(nil)

	var gvks []schema.GroupVersionKind
	for gvk := range c.objects {
		gvks = append(gvks, gvk)
	}

	sort.Slice(gvks, func(i, j int) bool {
		return gvks[i].String() < gvks[j].String()
	})

	for _, gvk := range gvks {
		if needSeparator {
			buf.WriteString("---\n")
		}
		needSeparator = true
		_, err := buf.Write(c.objects[gvk])
		if err != nil {
			return err
		}
	}
	return afero.WriteFile(c.fs, c.ManifestFile, buf.Bytes(), 0666)
}

// Status returns a nil client.StatusWriter.
func (c *manifestClient) Status() client.StatusWriter {
	return nil
}
