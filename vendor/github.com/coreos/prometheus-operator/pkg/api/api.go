// Copyright 2016 The prometheus-operator Authors
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

package api

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringclient "github.com/coreos/prometheus-operator/pkg/client/versioned"
	"github.com/coreos/prometheus-operator/pkg/k8sutil"
	"github.com/coreos/prometheus-operator/pkg/prometheus"
)

type API struct {
	kclient *kubernetes.Clientset
	mclient monitoringclient.Interface
	logger  log.Logger
}

func New(conf prometheus.Config, l log.Logger) (*API, error) {
	cfg, err := k8sutil.NewClusterConfig(conf.Host, conf.TLSInsecure, &conf.TLSConfig)
	if err != nil {
		return nil, errors.Wrap(err, "instantiating cluster config failed")
	}

	kclient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "instantiating kubernetes client failed")
	}

	mclient, err := monitoringclient.NewForConfig(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "instantiating monitoring client failed")
	}

	return &API{
		kclient: kclient,
		mclient: mclient,
		logger:  l,
	}, nil
}

var (
	prometheusRoute = regexp.MustCompile("/apis/monitoring.coreos.com/" + v1.Version + "/namespaces/(.*)/prometheuses/(.*)/status")
)

func (api *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if prometheusRoute.MatchString(req.URL.Path) {
			api.prometheusStatus(w, req)
		} else {
			w.WriteHeader(404)
		}
	})
}

type objectReference struct {
	name      string
	namespace string
}

func parsePrometheusStatusUrl(path string) objectReference {
	matches := prometheusRoute.FindAllStringSubmatch(path, -1)
	ns := ""
	name := ""
	if len(matches) == 1 {
		if len(matches[0]) == 3 {
			ns = matches[0][1]
			name = matches[0][2]
		}
	}

	return objectReference{
		name:      name,
		namespace: ns,
	}
}

func (api *API) prometheusStatus(w http.ResponseWriter, req *http.Request) {
	or := parsePrometheusStatusUrl(req.URL.Path)

	p, err := api.mclient.MonitoringV1().Prometheuses(or.namespace).Get(or.name, metav1.GetOptions{})
	if err != nil {
		if k8sutil.IsResourceNotFoundError(err) {
			w.WriteHeader(404)
		}
		api.logger.Log("error", err)
		return
	}

	p.Status, _, err = prometheus.PrometheusStatus(api.kclient, p)
	if err != nil {
		api.logger.Log("error", err)
	}

	b, err := json.Marshal(p)
	if err != nil {
		api.logger.Log("error", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(b)
}
