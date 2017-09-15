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

// Package controller contains the actual processing of frontend requests.
package controller

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/golang/glog"

	"istio.io/broker/pkg/model/osb"
)

const (
	demoCatalogFilePath = "example"
	catalogFileName     = "demo_catalog.json"
)

// Controller data
type Controller struct {
}

// CreateController creates a new controller instance.
func CreateController() (*Controller, error) {
	return new(Controller), nil
}

// Catalog serves catalog request and generate response.
func (c *Controller) Catalog(w http.ResponseWriter, _ *http.Request) {
	glog.Infof("Get Service Broker Catalog...")
	var catalog osb.Catalog

	if err := readAndUnmarshal(&catalog, demoCatalogFilePath, catalogFileName); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		writeResponse(w, http.StatusOK, catalog)
	}
}

// nolint: unparam
func writeResponse(w http.ResponseWriter, code int, object interface{}) {
	data, err := json.Marshal(object)
	if err != nil {
		glog.Errorf("Marsal response data object error %s", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(code)
	if _, err = fmt.Fprintf(w, string(data)); err != nil {
		glog.Errorf("Write response data error %s", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}
