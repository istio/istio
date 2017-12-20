// Copyright 2017 Istio Authors
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

package routing

import (
	"errors"
	"fmt"
	"strings"

	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/pkg/log"
)

// getDestination from the attribute bag, based on the id attribute.
func getDestination(attrs attribute.Bag, idAttribute string) (string, error) {

	v, ok := attrs.Get(idAttribute)
	if !ok {
		msg := fmt.Sprintf("%s identity not found in attributes%v", idAttribute, attrs.Names())
		log.Warnf(msg)
		return "", errors.New(msg)
	}

	var destination string
	if destination, ok = v.(string); !ok {
		msg := fmt.Sprintf("%s identity must be string: %v", idAttribute, v)
		log.Warnf(msg)
		return "", errors.New(msg)
	}

	return destination, nil
}

func getNamespace(destination string) string {

	// TODO: This is a potential garbage source on hot path. Consider fixing it.
	splits := strings.SplitN(destination, ".", 3) // we only care about service and namespace.
	if len(splits) > 1 {
		return splits[1]
	}
	return ""
}
