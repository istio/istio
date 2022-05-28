package main

import (
	"log"
	"regexp"
	"testing"

	"istio.io/istio/pkg/test/util/assert"
)

func TestClusterNameRegex(t *testing.T) {
	regexList, err := ReadClusterNameRegex(METRICS_FILENAME)
	if err != nil {
		log.Fatal(err)
	}
	regexString := BuildClusterNameRegex(regexList)
	regex, _ := regexp.Compile(regexString)

	type fields struct {
		input string
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{
			name:   "kubernetes service match upstream_cx_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.clusrr.local.upstream_cx_total"},
			want:   ".upstream_cx_total",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.clusrr.local.upstream_cx_total"},
			want:   ".upstream_cx_total",
		},
		{
			name:   "external service match upstream_cx_total",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_total"},
			want:   ".upstream_cx_total",
		},
		{
			name:   "kubernetes service match upstream_cx_active",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.clusrr.local.upstream_cx_active"},
			want:   ".upstream_cx_active",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_active",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.clusrr.local.upstream_cx_active"},
			want:   ".upstream_cx_active",
		},
		{
			name:   "external service match upstream_cx_active",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_active"},
			want:   ".upstream_cx_active",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, regex.FindString(test.fields.input))
		})
	}
}
