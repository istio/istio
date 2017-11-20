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
	"errors"
	"io/ioutil"
	"net/http"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	sc "google.golang.org/api/servicecontrol/v1"
)

type client struct {
	serviceControl *sc.Service
}

func (c *client) Check(serviceName string, request *sc.CheckRequest) (*sc.CheckResponse, error) {
	return c.serviceControl.Services.Check(serviceName, request).Do()
}

func (c *client) Report(serviceName string, request *sc.ReportRequest) (*sc.ReportResponse, error) {
	return c.serviceControl.Services.Report(serviceName, request).Do()
}

func (c *client) AllocateQuota(serviceName string, request *sc.AllocateQuotaRequest) (*sc.AllocateQuotaResponse, error) {
	return c.serviceControl.Services.AllocateQuota(serviceName, request).Do()
}

func getTokenSource(ctx context.Context, jsonKey []byte) (oauth2.TokenSource, error) {
	jwtCfg, err := google.JWTConfigFromJSON(jsonKey, sc.CloudPlatformScope, sc.ServicecontrolScope)
	if err != nil {
		return nil, err
	}
	return jwtCfg.TokenSource(ctx), nil
}

func getRawTokenBytes(credential string) ([]byte, error) {
	return ioutil.ReadFile(credential)
}

// Creates a service control client. The client is authenticated with service control with Oauth2.
func newClient(credentialPath string) (serviceControlClient, error) {
	token, err := getRawTokenBytes(credentialPath)
	if err != nil {
		return nil, err
	}

	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{
		Transport: http.DefaultTransport})

	tokenSrc, err := getTokenSource(ctx, token)
	if err != nil {
		return nil, err
	}

	httpClient := oauth2.NewClient(ctx, tokenSrc)
	if httpClient == nil {
		return nil, nil
	}

	svcClient, err := sc.New(httpClient)
	if err != nil {
		return nil, errors.New("fail to create ServiceControl client")
	}

	return &client{svcClient}, nil
}
