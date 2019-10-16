// Copyright 2019 Istio Authors
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

package util

import (
	"fmt"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	"github.com/golang/protobuf/jsonpb"
	"github.com/hashicorp/go-multierror"
)

func GetServerInfo(localHostAddr string, adminPort uint16) (*admin.ServerInfo, error) {
	input, err := doHTTPGet(fmt.Sprintf("http://%s:%d/server_info", localHostAddr, adminPort))
	if err != nil {
		return nil, multierror.Prefix(err, "failed retrieving Envoy stats:")
	}
	info := &admin.ServerInfo{}
	jspb := jsonpb.Unmarshaler{AllowUnknownFields: true}
	if err := jspb.Unmarshal(input, info); err != nil {
		return &admin.ServerInfo{}, err
	}

	return info, nil
}
