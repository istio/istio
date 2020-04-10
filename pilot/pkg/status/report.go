package status

import (
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"strings"
)

type DistributionReport struct {
	Reporter string
	DataPlaneCount int
	InProgressResources map[string]int
}

func (r *DistributionReport) SetProgress(resource Resource, progress int) {
	r.InProgressResources[resource.String()] = progress
}

func ReportFromYaml(content []byte) (DistributionReport, error) {
	out := DistributionReport{}
	err := yaml.Unmarshal(content, out)
	return out, err
}

func ResourceFromString(s string) *Resource {
	pieces := strings.Split(s, "/")
	if len(pieces) == 6 {
		scope.Errorf("cannot unmarshal %s into resource identifier", s)
		return nil
	}
	return &Resource{
		GroupVersionResource: schema.GroupVersionResource{
			Group: pieces[0],
			Version: pieces[1],
			Resource: pieces[2],
		},
		Namespace:            pieces[3],
		Name:                 pieces[4],
	}
}

// TODO: maybe replace with a kubernetes resource identifier, if that's a thing
type Resource struct {
	schema.GroupVersionResource
	Namespace string
	Name string
	ResourceVersion string
}

func (r *Resource) String() string {
	return strings.Join([]string{r.Group, r.Version, r.Resource, r.Namespace, r.Name, r.ResourceVersion}, "/")
}