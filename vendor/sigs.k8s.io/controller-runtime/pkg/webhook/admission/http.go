/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package admission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"k8s.io/api/admission/v1beta1"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
	"sigs.k8s.io/controller-runtime/pkg/webhook/internal/metrics"
)

var admissionv1beta1scheme = runtime.NewScheme()
var admissionv1beta1schemecodecs = serializer.NewCodecFactory(admissionv1beta1scheme)

func init() {
	addToScheme(admissionv1beta1scheme)
}

func addToScheme(scheme *runtime.Scheme) {
	utilruntime.Must(admissionv1beta1.AddToScheme(scheme))
}

var _ http.Handler = &Webhook{}

func (wh *Webhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTS := time.Now()
	defer metrics.RequestLatency.WithLabelValues(wh.Name).Observe(time.Now().Sub(startTS).Seconds())

	var body []byte
	var err error

	var reviewResponse types.Response
	if r.Body != nil {
		if body, err = ioutil.ReadAll(r.Body); err != nil {
			log.Error(err, "unable to read the body from the incoming request")
			reviewResponse = ErrorResponse(http.StatusBadRequest, err)
			wh.writeResponse(w, reviewResponse)
			return
		}
	} else {
		err = errors.New("request body is empty")
		log.Error(err, "bad request")
		reviewResponse = ErrorResponse(http.StatusBadRequest, err)
		wh.writeResponse(w, reviewResponse)
		return
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		err = fmt.Errorf("contentType=%s, expect application/json", contentType)
		log.Error(err, "unable to process a request with an unknown content type", "content type", contentType)
		reviewResponse = ErrorResponse(http.StatusBadRequest, err)
		wh.writeResponse(w, reviewResponse)
		return
	}

	ar := v1beta1.AdmissionReview{}
	if _, _, err := admissionv1beta1schemecodecs.UniversalDeserializer().Decode(body, nil, &ar); err != nil {
		log.Error(err, "unable to decode the request")
		reviewResponse = ErrorResponse(http.StatusBadRequest, err)
		wh.writeResponse(w, reviewResponse)
		return
	}

	// TODO: add panic-recovery for Handle
	reviewResponse = wh.Handle(context.Background(), types.Request{AdmissionRequest: ar.Request})
	wh.writeResponse(w, reviewResponse)
}

func (wh *Webhook) writeResponse(w io.Writer, response types.Response) {
	if response.Response.Result.Code != 0 {
		if response.Response.Result.Code == http.StatusOK {
			metrics.TotalRequests.WithLabelValues(wh.Name, "true").Inc()
		} else {
			metrics.TotalRequests.WithLabelValues(wh.Name, "false").Inc()
		}
	}

	encoder := json.NewEncoder(w)
	responseAdmissionReview := v1beta1.AdmissionReview{
		Response: response.Response,
	}
	err := encoder.Encode(responseAdmissionReview)
	if err != nil {
		log.Error(err, "unable to encode the response")
		wh.writeResponse(w, ErrorResponse(http.StatusInternalServerError, err))
	}
}
