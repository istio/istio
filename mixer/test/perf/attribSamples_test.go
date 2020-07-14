// Copyright Istio Authors
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

package perftests

import (
	"time"
)

var (

	// one sample set of attributes that are passed by the envoy in the bookinfo example.
	baseAttr = map[string]interface{}{
		"source.service":      "AcmeService",
		"response.code":       int64(111),
		"request.size":        int64(222),
		"request.time":        time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC),
		"context.protocol":    "http",
		"check.cache_hit":     false,
		"connection.mtls":     false,
		"destination.labels":  map[string]string{"version": "v1", "app": "details", "pod-template-hash": "2854595222"},
		"destination.service": "details.default.svc.cluster.local",
		"destination.uid":     "kubernetes://details-v1-6d989f9666-289gr.default",
		"quota.cache_hit":     false,
		"request.headers": map[string]string{"x-b3-parentspanid": "e3afaeba60c58aaa",
			"authority": "details:9080", "user-agent": "python-requests/2.18.4",
			"x-b3-traceid": "e3afaeba60c58aaa", "accept-encoding": "gzip, deflate",
			":path": "/details/0", ":method": "GET", "x-forwarded-proto": "http",
			"content-length": "0", "x-b3-spanid": "e9ab222e0b5608f3",
			"x-request-id": "716f54f9-2f0f-93ad-b671-911fc9c57431", "accept": "*/* x-b3-sampled:1"},

		"request.host":      "details:9080",
		"request.method":    "GET",
		"request.path":      "/details/0",
		"request.scheme":    "http",
		"request.useragent": "python-requests/2.18.4",
		"response.headers": map[string]string{"content-type": "application/json",
			"date":   "Tue, 08 May 2018 19:31:54 GMT",
			"server": "envoy", "status": "200",
			"x-envoy-upstream-service-time": "2", "content-length": "178"},
		"response.size": int64(178),
		"source.labels": map[string]string{"app": "productpage", "pod-template-hash": "3666554850", "version": "v1"},
		"source.uid":    "kubernetes://productpage-v1-7bbb998d94-g85pc.default",
	}

	// attr1-through-5 replaces baseAttr's source, destination, request.size and response.code attributes to `1-to-5` prefix based
	// values
	attr1 = replaceAttrs(baseAttr, map[string]interface{}{
		"source.service":      "AcmeService1",
		"source.labels":       map[string]string{"version": "111"},
		"destination.service": "DevNullService1",
		"destination.labels":  map[string]string{"app": "details", "version": "111"},
		"response.code":       int64(111),
		"request.size":        int64(111),
	})

	attr2 = replaceAttrs(baseAttr, map[string]interface{}{
		"source.service":      "AcmeService2",
		"source.labels":       map[string]string{"version": "222"},
		"destination.service": "DevNullService2",
		"destination.labels":  map[string]string{"app": "details", "version": "222"},
		"response.code":       int64(222),
		"request.size":        int64(222),
	})

	attr3 = replaceAttrs(baseAttr, map[string]interface{}{
		"source.service":      "AcmeService3",
		"source.labels":       map[string]string{"version": "333"},
		"destination.service": "DevNullService3",
		"destination.labels":  map[string]string{"app": "details", "version": "333"},
		"response.code":       int64(333),
		"request.size":        int64(333),
	})

	attr4 = replaceAttrs(baseAttr, map[string]interface{}{
		"source.service":      "AcmeService4",
		"source.labels":       map[string]string{"version": "444"},
		"destination.service": "DevNullService4",
		"destination.labels":  map[string]string{"app": "details", "version": "444"},
		"response.code":       int64(444),
		"request.size":        int64(444),
	})

	attr5 = replaceAttrs(baseAttr, map[string]interface{}{
		"source.service":      "AcmeService5",
		"source.labels":       map[string]string{"version": "555"},
		"destination.service": "DevNullService5",
		"destination.labels":  map[string]string{"app": "details", "version": "555"},
		"response.code":       int64(555),
		"request.size":        int64(555),
	})
)

func replaceAttrs(base map[string]interface{}, override map[string]interface{}) map[string]interface{} {
	newAttrs := base
	for k, v := range override {
		newAttrs[k] = v
	}

	return newAttrs
}
