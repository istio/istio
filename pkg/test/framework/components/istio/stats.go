package istio

import (
	"context"
	"fmt"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/resource"
	"strings"
)

func (i *operatorComponent) ControlPlaneStatsOrFail(f test.Failer, cluster resource.Cluster) map[string]*dto.MetricFamily {
	res, err := i.ControlPlaneStats(cluster)
	if err != nil {
		f.Fatalf("failed getting control plane stats for cluster %s: %v", cluster.Name(), err)
	}
	return res
}
func (i *operatorComponent) ControlPlaneStats(cluster resource.Cluster) (map[string]*dto.MetricFamily, error) {
	cp, err := i.environment.GetControlPlaneCluster(cluster)
	if err != nil {
		return nil, err
	}

	podNamespace := i.settings.SystemNamespace
	pods, err := cp.GetIstioPods(context.TODO(), podNamespace, map[string]string{
		"labelSelector": "istio=pilot",
		"fieldSelector": "status.phase=Running",
	})
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		return nil, fmt.Errorf("no running istiod pods in %s", i.settings.SystemNamespace)
	}

	// Exec onto the pod and make a curl request to the admin port, writing
	podName := pods[0].Name
	command := "curl localhost:8080/metrics"
	stdout, stderr, err := cp.PodExec(podName, podNamespace, "discovery", command)
	if err != nil {
		return nil, fmt.Errorf("failed exec on pod %s/%s: %v. Command: %s. Output:\n%s",
			podNamespace, podName, err, command, stdout+stderr)
	}

	parser := expfmt.TextParser{}
	mfMap, err := parser.TextToMetricFamilies(strings.NewReader(stdout))
	if err != nil {
		return nil, fmt.Errorf("failed parsing prometheus stats: %v", err)
	}
	return mfMap, nil
}