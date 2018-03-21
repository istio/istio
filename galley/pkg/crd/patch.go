//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package crd

import (
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	"k8s.io/client-go/rest"
)

var newForConfig = clientset.NewForConfig

var getCustomResourceDefinitionsInterface = func(cfg *rest.Config) (v1beta1.CustomResourceDefinitionInterface, error) {
	c, err := newForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return c.ApiextensionsV1beta1().CustomResourceDefinitions(), nil
}
