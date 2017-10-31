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

package svcctrl

import (
	"net/http"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	sc "google.golang.org/api/servicecontrol/v1"

	"istio.io/istio/mixer/pkg/adapter"
)

// Creates a service control client. The client is authenticated with service control with Oauth2.
func createClient(logger adapter.Logger) (*sc.Service, error) {
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{
		Transport: http.DefaultTransport})

	client, err := google.DefaultClient(ctx, sc.CloudPlatformScope, sc.ServicecontrolScope)
	if err != nil {
		return nil, logger.Errorf("unable to create http client %v", err.Error())
	}

	serviceControl, err := sc.New(client)
	if err != nil {
		return nil, logger.Errorf("unable to create service control client %v", err.Error())
	}
	logger.Infof("created service control client\n")
	return serviceControl, nil
}
