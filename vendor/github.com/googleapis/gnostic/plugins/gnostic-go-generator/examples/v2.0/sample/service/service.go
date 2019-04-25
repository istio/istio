/*
 Copyright 2018 Google Inc. All Rights Reserved.

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

package main

import (
	"github.com/googleapis/gnostic/plugins/gnostic-go-generator/examples/v2.0/sample/sample"
)

//
// The Service type implements a sample service.
//
type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (service *Service) GetSample(parameters *sample.GetSampleParameters, responses *sample.GetSampleResponses) (err error) {
	(*responses).OK = &sample.Sample{
		Id:    parameters.Id,
		Thing: map[string]interface{}{"thing": 123},
		Count: int32(len(parameters.Id))}
	return err
}
