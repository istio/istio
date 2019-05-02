// Copyright 2019 The prometheus-operator Authors
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

package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"os"

	"github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/ghodss/yaml"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func main() {
	files := os.Args[1:]

	for _, filename := range files {
		content, err := ioutil.ReadFile(filename)
		if err != nil {
			log.Fatal(err)
		}

		var meta metav1.TypeMeta

		err = yaml.Unmarshal(content, &meta)
		if err != nil {
			log.Fatal(err)
		}

		switch meta.Kind {
		case v1.AlertmanagersKind:
			j, err := yaml.YAMLToJSON(content)
			if err != nil {
				log.Fatalf("unable to convert YAML to JSON: %v", err)
			}

			decoder := json.NewDecoder(bytes.NewBuffer(j))
			decoder.DisallowUnknownFields()

			var alertmanager v1.Alertmanager
			err = decoder.Decode(&alertmanager)
			if err != nil {
				log.Fatalf("alertmanager is invalid: %v", err)
			}
		case v1.PrometheusesKind:
			j, err := yaml.YAMLToJSON(content)
			if err != nil {
				log.Fatalf("unable to convert YAML to JSON: %v", err)
			}

			decoder := json.NewDecoder(bytes.NewBuffer(j))
			decoder.DisallowUnknownFields()

			var prometheus v1.Prometheus
			err = decoder.Decode(&prometheus)
			if err != nil {
				log.Fatalf("prometheus is invalid: %v", err)
			}
		case v1.PrometheusRuleKind:
			j, err := yaml.YAMLToJSON(content)
			if err != nil {
				log.Fatalf("unable to convert YALM to JSON: %v", err)
			}

			decoder := json.NewDecoder(bytes.NewBuffer(j))
			decoder.DisallowUnknownFields()

			var rule v1.PrometheusRule
			err = decoder.Decode(&rule)
			if err != nil {
				log.Fatalf("prometheus rule is invalid: %v", err)
			}
		case v1.ServiceMonitorsKind:
			j, err := yaml.YAMLToJSON(content)
			if err != nil {
				log.Fatalf("unable to convert YALM to JSON: %v", err)
			}

			decoder := json.NewDecoder(bytes.NewBuffer(j))
			decoder.DisallowUnknownFields()

			var serviceMonitor v1.ServiceMonitor
			err = decoder.Decode(&serviceMonitor)
			if err != nil {
				log.Fatalf("serviceMonitor is invalid: %v", err)
			}
		default:
			log.Fatal("MetaType is unknown to linter. Not in Alertmanager, Prometheus, PrometheusRule, ServiceMonitor")
		}
	}
}
