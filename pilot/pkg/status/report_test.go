package status

import (
	"reflect"
	"testing"

	"github.com/onsi/gomega"

	"gopkg.in/yaml.v2"
)

func TestReportSerialization(t *testing.T) {
	in := DistributionReport{
		Reporter:       "Me",
		DataPlaneCount: 10,
		InProgressResources: map[string]int{
			(&Resource{
				Name:      "water",
				Namespace: "default",
			}).String(): 1,
		},
	}
	outbytes, err := yaml.Marshal(in)
	gomega.Expect(err).To(gomega.BeNil())
	out := DistributionReport{}
	err = yaml.Unmarshal(outbytes, &out)
	gomega.Expect(err).To(gomega.BeNil())
	if !reflect.DeepEqual(out, in) {
		t.Errorf("Report Serialization mutated the Report. got = %v, want %v", out, in)
	}
}
