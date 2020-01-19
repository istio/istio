// Code generated for package k8smeta by go-bindata DO NOT EDIT. (@generated)
// sources:
// k8smeta.yaml
package k8smeta

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)
type asset struct {
	bytes []byte
	info  os.FileInfo
}

type bindataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

// Name return file name
func (fi bindataFileInfo) Name() string {
	return fi.name
}

// Size return file size
func (fi bindataFileInfo) Size() int64 {
	return fi.size
}

// Mode return file mode
func (fi bindataFileInfo) Mode() os.FileMode {
	return fi.mode
}

// Mode return file modify time
func (fi bindataFileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir return file whether a directory
func (fi bindataFileInfo) IsDir() bool {
	return fi.mode&os.ModeDir != 0
}

// Sys return file is sys mode
func (fi bindataFileInfo) Sys() interface{} {
	return nil
}

var _k8smetaYaml = []byte(`# Copyright 2019 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in conformance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


collections:
  # Built-in K8s collections
  - name: "k8s/core/v1/endpoints"
    kind: "Endpoints"

  - name: "k8s/core/v1/namespaces"
    kind: "Namespace"

  - name: "k8s/core/v1/nodes"
    kind: "Node"

  - name: "k8s/core/v1/pods"
    kind: "Pod"

  - name: "k8s/core/v1/services"
    kind: "Service"

  - name: "k8s/extensions/v1beta1/ingresses"
    kind: "Ingress"
    group: "extensions"

  - name: "k8s/apps/v1/deployments"
    kind: "Deployment"
    group: "apps"

resources:
  - collection: "k8s/extensions/v1beta1/ingresses"
    kind: "Ingress"
    plural: "ingresses"
    group: "extensions"
    version: "v1beta1"
    proto: "k8s.io.api.extensions.v1beta1.IngressSpec"
    protoPackage: "k8s.io/api/extensions/v1beta1"

  - collection: "k8s/core/v1/services"
    kind: "Service"
    plural: "services"
    version: "v1"
    proto: "k8s.io.api.core.v1.ServiceSpec"
    protoPackage: "k8s.io/api/core/v1"

  - collection: "k8s/core/v1/namespaces"
    kind: "Namespace"
    plural: "namespaces"
    version: "v1"
    proto: "k8s.io.api.core.v1.NamespaceSpec"
    protoPackage: "k8s.io/api/core/v1"

  - collection: "k8s/core/v1/nodes"
    kind: "Node"
    plural: "nodes"
    version: "v1"
    proto: "k8s.io.api.core.v1.NodeSpec"
    protoPackage: "k8s.io/api/core/v1"

  - collection: "k8s/core/v1/pods"
    kind: "Pod"
    plural: "pods"
    version: "v1"
    proto: "k8s.io.api.core.v1.Pod"
    protoPackage: "k8s.io/api/core/v1"

  - collection: "k8s/core/v1/endpoints"
    kind: "Endpoints"
    plural: "endpoints"
    version: "v1"
    proto: "k8s.io.api.core.v1.Endpoints"
    protoPackage: "k8s.io/api/core/v1"

  - collection: "k8s/apps/v1/deployments"
    kind: "Deployment"
    plural: "deployments"
    group: "apps"
    version: "v1"
    proto: "k8s.io.api.apps.v1.Deployment"
    protoPackage: "k8s.io/api/apps/v1"

transforms:
  - type: direct
    mapping:
`)

func k8smetaYamlBytes() ([]byte, error) {
	return _k8smetaYaml, nil
}

func k8smetaYaml() (*asset, error) {
	bytes, err := k8smetaYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "k8smeta.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("Asset %s can't read by error: %v", name, err)
		}
		return a.bytes, nil
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// MustAsset is like Asset but panics when Asset would return an error.
// It simplifies safe initialization of global variables.
func MustAsset(name string) []byte {
	a, err := Asset(name)
	if err != nil {
		panic("asset: Asset(" + name + "): " + err.Error())
	}

	return a
}

// AssetInfo loads and returns the asset info for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func AssetInfo(name string) (os.FileInfo, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("AssetInfo %s can't read by error: %v", name, err)
		}
		return a.info, nil
	}
	return nil, fmt.Errorf("AssetInfo %s not found", name)
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() (*asset, error){
	"k8smeta.yaml": k8smetaYaml,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
		cannonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(cannonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for childName := range node.Children {
		rv = append(rv, childName)
	}
	return rv, nil
}

type bintree struct {
	Func     func() (*asset, error)
	Children map[string]*bintree
}

var _bintree = &bintree{nil, map[string]*bintree{
	"k8smeta.yaml": &bintree{k8smetaYaml, map[string]*bintree{}},
}}

// RestoreAsset restores an asset under the given directory
func RestoreAsset(dir, name string) error {
	data, err := Asset(name)
	if err != nil {
		return err
	}
	info, err := AssetInfo(name)
	if err != nil {
		return err
	}
	err = os.MkdirAll(_filePath(dir, filepath.Dir(name)), os.FileMode(0755))
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(_filePath(dir, name), data, info.Mode())
	if err != nil {
		return err
	}
	err = os.Chtimes(_filePath(dir, name), info.ModTime(), info.ModTime())
	if err != nil {
		return err
	}
	return nil
}

// RestoreAssets restores an asset under the given directory recursively
func RestoreAssets(dir, name string) error {
	children, err := AssetDir(name)
	// File
	if err != nil {
		return RestoreAsset(dir, name)
	}
	// Dir
	for _, child := range children {
		err = RestoreAssets(dir, filepath.Join(name, child))
		if err != nil {
			return err
		}
	}
	return nil
}

func _filePath(dir, name string) string {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	return filepath.Join(append([]string{dir}, strings.Split(cannonicalName, "/")...)...)
}
