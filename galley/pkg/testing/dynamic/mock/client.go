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

package mock

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/flowcontrol"

	"istio.io/istio/galley/pkg/testing/common"
)

// Client is a mock implementation of dynamic.Interface.
type Client struct {
	e *common.MockLog

	MockResource *ResourceInterface
}

var _ dynamic.Interface = &Client{}

// NewClient returns a new instance of a Client.
func NewClient() *Client {
	e := &common.MockLog{}
	return &Client{
		e:            e,
		MockResource: &ResourceInterface{e: e},
	}
}

// String returns the log contents for this mock
func (m *Client) String() string {
	return m.e.String()
}

// GetRateLimiter implementation
func (m *Client) GetRateLimiter() flowcontrol.RateLimiter {
	panic("Not implemented: GetRateLimiter")
}

// Resource implementation
func (m *Client) Resource(resource *metav1.APIResource, namespace string) dynamic.ResourceInterface {
	return m.MockResource
}

// ParameterCodec implementation
func (m *Client) ParameterCodec(parameterCodec runtime.ParameterCodec) dynamic.Interface {
	return m
}
