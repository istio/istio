// Copyright 2019 The OpenZipkin Authors
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

/*
Package propagation holds the required function signatures for Injection and
Extraction as used by the Zipkin Tracer.

Subpackages of this package contain officially supported standard propagation
implementations.
*/
package propagation

import "github.com/openzipkin/zipkin-go/model"

// Extractor function signature
type Extractor func() (*model.SpanContext, error)

// Injector function signature
type Injector func(model.SpanContext) error
