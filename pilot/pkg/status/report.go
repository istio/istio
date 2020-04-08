package status

import "gopkg.in/yaml.v2"

type DistributionReport struct {
	Reporter string
	DataPlaneCount int
	InProgressResources map[Resource]int
}

func ReportFromYaml(content []byte) (DistributionReport, error) {
	out := DistributionReport{}
	err := yaml.Unmarshal(content, out)
	return out, err
}

// TODO: maybe replace with a kubernetes resource identifier, if that's a thing
type Resource string