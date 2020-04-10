package status

import (
	"fmt"
	"reflect"
	"testing"

	"gopkg.in/yaml.v2"
	"gotest.tools/assert"
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
	assert.NilError(t, err)
	fmt.Println(string(outbytes))
	out := DistributionReport{}
	err = yaml.Unmarshal(outbytes, &out)
	assert.NilError(t, err)
	if !reflect.DeepEqual(out, in) {
		t.Errorf("Report Serialization mutated the Report. got = %v, want %v", out, in)
	}
}
