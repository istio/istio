// Copyright 2016 Google Inc.
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

package cmd

import (
	"fmt"
	"os"
	"strings"

	"google.golang.org/grpc"

	mixerpb "istio.io/mixer/api/v1"
)

type clientState struct {
	client     mixerpb.MixerClient
	connection *grpc.ClientConn
}

func createAPIClient(port string) (*clientState, error) {
	cs := clientState{}

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())

	var err error
	if cs.connection, err = grpc.Dial(port, opts...); err != nil {
		return nil, err
	}

	cs.client = mixerpb.NewMixerClient(cs.connection)
	return &cs, nil
}

func deleteAPIClient(cs *clientState) {
	cs.connection.Close()
	cs.client = nil
	cs.connection = nil
}

func parseAttributes(attributes string) (map[string]string, error) {
	attrs := make(map[string]string)
	if len(attributes) > 0 {
		for _, a := range strings.Split(attributes, ",") {
			i := strings.Index(a, "=")
			if i < 0 {
				return nil, fmt.Errorf("Attribute value %v does not include an = sign", a)
			} else if i == 0 {
				return nil, fmt.Errorf("Attribute value %v does not contain a valid name", a)
			}
			name := a[0:i]
			value := a[i+1:]
			attrs[name] = value
		}
	}

	return attrs, nil
}

func errorf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}
