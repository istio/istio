// Copyright 2018 The Operator-SDK Authors
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

package tlsutil

import "errors"

var (
	ErrCANotFound        = errors.New("no CA secret and ConfigMap found")
	ErrCAKeyAndCACertReq = errors.New("a CA key and CA cert need to be provided when requesting a custom CA")
	ErrInternal          = errors.New("internal error while generating TLS assets")
	// TODO: add other tls util errors.
)
