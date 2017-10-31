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

	"istio.io/istio/broker/pkg/model/config"
	"istio.io/istio/broker/pkg/model/osb"
)

// Controller data
type Controller struct {
	config.BrokerConfigStore
}

// CreateController creates a new controller instance.
func CreateController(config config.BrokerConfigStore) (*Controller, error) {
	return &Controller{config}, nil
}

// Catalog serves catalog request and generate response.
func (c *Controller) Catalog(w http.ResponseWriter, _ *http.Request) {
	glog.Infof("Fetching Service Broker Catalog...")
	cat := c.catalog()
	glog.V(2).Infof("Got catalog\n %#v", cat)
	writeResponse(w, http.StatusOK, cat)
}

func (c *Controller) catalog() *osb.Catalog {
	jc := new(osb.Catalog)
	sc := c.ServiceClasses()
	for k, s := range sc {
		glog.V(2).Infof("loading service %q", k)
		js := osb.NewService(s)
		for pk, p := range c.ServicePlansByService(k) {
			glog.V(2).Infof("loading service plan %q", pk)
			jp := osb.NewServicePlan(p)
			js.AddPlan(jp)
		}
		jc.AddService(js)
	}
	return jc
}

// nolint: unparam
func writeResponse(w http.ResponseWriter, code int, object interface{}) {
	data, err := json.Marshal(object)
	if err != nil {
		glog.Errorf("Marsal response data object error %s", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(code)
	if _, err = fmt.Fprintf(w, string(data)); err != nil {
		glog.Errorf("Write response data error %s", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}
