package echotest

import "istio.io/istio/pkg/test/framework/components/echo"

type filterFunc func(echo.Instances) echo.Instances

// FilterSources applies each of the filter funcitons in order to allow removing workloads from the set of clients.
func (t *T) FilterSources(filters ...filterFunc) {
	for _, filter := range filters {
		t.sources = filter(t.sources)
	}
}

// FilterDestinations applies each of the filter funcitons in order to allow removing workloads from the set of targets.
func (t *T) FilterDestinations(filters ...filterFunc) {
	for _, filter := range filters {
		t.destinations = filter(t.destinations)
	}
}

// OneRegularPod finds the first Pod deployment that has a sidecar and doesn't use a headless service and removes all
// other "regular" pods that aren't part of the same Service. Pods that are part of the same Service but are in a
// different cluster or revision will still be included.
func OneRegularPod(instances echo.Instances) echo.Instances {
	var out echo.Instances
	var key *echo.Deployment
	for _, instance := range instances {
		if key == nil {
			if isRegularPod(instance) {
				k := instance.Config().DeploymentKey()
				key = &k
			}
		} else {
			if isRegularPod(instance) && instance.Config().DeploymentKey() != *key {
				continue
			}
		}
		out = append(out, instance)
	}
	return out
}

func isRegularPod(instance echo.Instance) bool {
	c := instance.Config()
	isNaked := len(c.Subsets) > 0 && c.Subsets[0].Annotations != nil && !c.Subsets[0].Annotations.GetBool(echo.SidecarInject)
	// TODO not sure about subsets == 1 being part of this?
	return !c.DeployAsVM && !c.Headless && len(c.Subsets) == 1 && !isNaked
}
