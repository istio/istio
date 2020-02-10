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

package stackdriver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	cloudtrace "google.golang.org/api/cloudtrace/v1"

	"context"
	"flag"
	"os"
	"regexp"
	"strings"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/resource"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/tests/util"
)

const (
	appName         = "stackdriver"
	tracesAPI       = "/api/v2/traces?limit=%d&spanName=%s&annotationQuery=%s"
	stackdriverPort = 9411
)

var (
	_          Instance  = &kubeComponent{}
	_          io.Closer = &kubeComponent{}
	gcpProj    *string
	saCredPath *string
)

type kubeComponent struct {
	id        resource.ID
	address   string
	forwarder testKube.PortForwarder
	env       *kube.Environment
}

func newKube(ctx resource.Context) (Instance, error) {
	env := ctx.Environment().(*kube.Environment)
	c := &kubeComponent{
		env: env,
	}
	c.id = ctx.TrackResource(c)
	gcpProj = flag.String("gcp_proj", os.Getenv("GCP_PROJ"), "GCP Project ID, required")
	saCredPath = flag.String("sa_cred", "", "Path to the service account credential.")
	flag.Parse()
	if *gcpProj == "" {
		*gcpProj = getGCloudProjectID()
	}
	if *saCredPath != "" {
		if _, err := os.Stat(*saCredPath); !os.IsNotExist(err) {
			os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", *saCredPath)
		}
	}
	return c, nil
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

func (c *kubeComponent) QueryTraces(limit int, spanName, annotationQuery string) ([]Trace, error) {
	ctx := context.Background()
	s, err := createTraceService(ctx)
	if err != nil {
		t.Fatal(err)
	}

	f := fmt.Sprintf("+source_workload_namespace:%s", tc.Kube.Namespace)
	t.Logf("Trace filter: %v", f)
	startTime := time.Now().UTC().Add(time.Minute * -10)
	endTime := time.Now().UTC()

	resp, err := s.Projects.Traces.List(*gcpProj).
		Filter(f).
		StartTime(startTime.Format(time.RFC3339Nano)).
		EndTime(endTime.Format(time.RFC3339Nano)).
		PageSize(10).
		Do()
	if err != nil {
		t.Fatalf("Could not query traces, %v ", err)
	}

	if len(resp.Traces) == 0 {
		t.Fatal("No Traces found")
	}
	t.Logf("Got %v traces", len(resp.Traces))
}

// Close implements io.Closer.
func (c *kubeComponent) Close() error {
	return c.forwarder.Close()
}

func extractTraces(resp []byte) ([]Trace, error) {
	var traceObjs []interface{}
	if err := json.Unmarshal(resp, &traceObjs); err != nil {
		return []Trace{}, err
	}
	var ret []Trace
	for _, t := range traceObjs {
		spanObjs, ok := t.([]interface{})
		if !ok || len(spanObjs) == 0 {
			scopes.Framework.Debugf("cannot parse or cannot find spans in trace object %+v", t)
			continue
		}
		var spans []Span
		for _, obj := range spanObjs {
			newSpan := buildSpan(obj)
			spans = append(spans, newSpan)
		}
		for p := range spans {
			for c := range spans {
				if spans[c].ParentSpanID == spans[p].SpanID {
					spans[p].ChildSpans = append(spans[p].ChildSpans, &spans[c])
				}
			}
			// make order of child spans deterministic
			sort.Slice(spans[p].ChildSpans, func(i, j int) bool {
				return spans[p].ChildSpans[i].Name < spans[p].ChildSpans[j].Name
			})
		}
		ret = append(ret, Trace{Spans: spans})
	}
	if len(ret) > 0 {
		return ret, nil
	}
	return []Trace{}, errors.New("cannot find any traces")
}

func buildSpan(obj interface{}) Span {
	var s Span
	spanSpec := obj.(map[string]interface{})
	if spanID, ok := spanSpec["id"]; ok {
		s.SpanID = spanID.(string)
	}
	if parentSpanID, ok := spanSpec["parentId"]; ok {
		s.ParentSpanID = parentSpanID.(string)
	}
	if endpointObj, ok := spanSpec["localEndpoint"]; ok {
		if em, ok := endpointObj.(map[string]interface{}); ok {
			s.ServiceName = em["serviceName"].(string)
		}
	}
	if name, ok := spanSpec["name"]; ok {
		s.Name = name.(string)
	}
	return s
}

func getGCloudProjectID() string {
	cmd := fmt.Sprintf("gcloud config list core/project")
	output, err := util.ShellMuteOutput(cmd)
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(".*project = (.*?)\n.*")
	match := re.FindStringSubmatch(output)
	if len(match) < 1 {
		return ""
	}
	ret := strings.TrimSpace(match[1])
	return ret
}

func createTraceService(ctx context.Context) (*cloudtrace.Service, error) {
	s, err := cloudtrace.NewService(ctx)
	if err != nil {
		return nil, err
	}
	return s, nil
}
