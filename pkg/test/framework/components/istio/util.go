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

package istio

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"google.golang.org/protobuf/types/known/structpb"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/cleanup"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/protomarshal"
)

const (
	istiodLabel = "pilot"
)

var dummyValidationVirtualServiceTemplate = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: validation-readiness-dummy-virtual-service
  namespace: %s
spec:
  hosts:
    - non-existent-host
  http:
    - route:
      - destination:
          host: non-existent-host
          subset: v1
        weight: 75
      - destination:
          host: non-existent-host
          subset: v2
        weight: 25
`

func waitForValidationWebhook(ctx resource.Context, cluster cluster.Cluster, cfg Config) error {
	dummyValidationVirtualService := fmt.Sprintf(dummyValidationVirtualServiceTemplate, cfg.SystemNamespace)
	defer func() {
		e := ctx.ConfigKube(cluster).YAML("", dummyValidationVirtualService).Delete()
		if e != nil {
			scopes.Framework.Warnf("error deleting dummy virtual service for waiting the validation webhook: %v", e)
		}
	}()

	scopes.Framework.Info("Creating dummy virtual service to check for validation webhook readiness")
	return retry.UntilSuccess(func() error {
		err := ctx.ConfigKube(cluster).YAML("", dummyValidationVirtualService).Apply()
		if err == nil {
			return nil
		}

		return fmt.Errorf("validation webhook not ready yet: %v", err)
	}, retry.Timeout(time.Minute))
}

func (i *operatorComponent) RemoteDiscoveryAddressFor(cluster cluster.Cluster) (net.TCPAddr, error) {
	var addr net.TCPAddr
	primary := cluster.Primary()
	if !primary.IsConfig() {
		// istiod is exposed via LoadBalancer since we won't have ingress outside of a cluster;a cluster that is;
		// a control cluster, but not config cluster is supposed to simulate istiod outside of k8s or "external"
		address, err := retry.UntilComplete(func() (interface{}, bool, error) {
			return getRemoteServiceAddress(i.environment.Settings(), primary, i.settings.SystemNamespace, istiodLabel,
				istiodSvcName, discoveryPort)
		}, getAddressTimeout, getAddressDelay)
		if err != nil {
			return net.TCPAddr{}, err
		}
		addr = address.(net.TCPAddr)
	} else {
		name := types.NamespacedName{Name: eastWestIngressServiceName, Namespace: i.settings.SystemNamespace}
		addr = i.CustomIngressFor(primary, name, eastWestIngressIstioLabel).DiscoveryAddress()
	}
	if addr.IP.String() == "<nil>" {
		return net.TCPAddr{}, fmt.Errorf("failed to get ingress IP for %s", primary.Name())
	}
	return addr, nil
}

func getRemoteServiceAddress(s *kube.Settings, cluster cluster.Cluster, ns, label, svcName string,
	port int,
) (interface{}, bool, error) {
	if !s.LoadBalancerSupported {
		pods, err := cluster.PodsForSelector(context.TODO(), ns, label)
		if err != nil {
			return nil, false, err
		}

		names := make([]string, 0, len(pods.Items))
		for _, p := range pods.Items {
			names = append(names, p.Name)
		}
		scopes.Framework.Debugf("Querying remote service %s, pods:%v", svcName, names)
		if len(pods.Items) == 0 {
			return nil, false, fmt.Errorf("no remote service pod found")
		}

		scopes.Framework.Debugf("Found pod: %v", pods.Items[0].Name)
		ip := pods.Items[0].Status.HostIP
		if ip == "" {
			return nil, false, fmt.Errorf("no Host IP available on the remote service node yet")
		}

		svc, err := cluster.Kube().CoreV1().Services(ns).Get(context.TODO(), svcName, v1.GetOptions{})
		if err != nil {
			return nil, false, err
		}

		if len(svc.Spec.Ports) == 0 {
			return nil, false, fmt.Errorf("no ports found in service: %s/%s", ns, svcName)
		}

		var nodePort int32
		for _, svcPort := range svc.Spec.Ports {
			if svcPort.Protocol == "TCP" && svcPort.Port == int32(port) {
				nodePort = svcPort.NodePort
				break
			}
		}
		if nodePort == 0 {
			return nil, false, fmt.Errorf("no port %d found in service: %s/%s", port, ns, svcName)
		}

		return net.TCPAddr{IP: net.ParseIP(ip), Port: int(nodePort)}, true, nil
	}

	// Otherwise, get the load balancer IP.
	svc, err := cluster.Kube().CoreV1().Services(ns).Get(context.TODO(), svcName, v1.GetOptions{})
	if err != nil {
		return nil, false, err
	}

	if len(svc.Status.LoadBalancer.Ingress) == 0 {
		return nil, false, fmt.Errorf("service %s/%s is not available yet: no ingress", svc.Namespace, svc.Name)
	}
	ingr := svc.Status.LoadBalancer.Ingress[0]
	if ingr.IP == "" && ingr.Hostname == "" {
		return nil, false, fmt.Errorf("service %s/%s is not available yet: no ingress", svc.Namespace, svc.Name)
	}
	if ingr.IP != "" {
		return net.TCPAddr{IP: net.ParseIP(ingr.IP), Port: port}, true, nil
	}
	return net.JoinHostPort(ingr.Hostname, strconv.Itoa(port)), true, nil
}

func (i *operatorComponent) isExternalControlPlane() bool {
	for _, c := range i.ctx.AllClusters() {
		if c.IsPrimary() && !c.IsConfig() {
			return true
		}
	}
	return false
}

func UpdateMeshConfig(t resource.Context, ns string, clusters cluster.Clusters,
	update func(*meshconfig.MeshConfig) error, cleanupStrategy cleanup.Strategy,
) error {
	errG := multierror.Group{}
	origCfg := map[string]string{}
	mu := sync.RWMutex{}

	cmName := "istio"
	if rev := t.Settings().Revisions.Default(); rev != "default" && rev != "" {
		cmName += "-" + rev
	}
	for _, c := range clusters.Kube() {
		c := c
		errG.Go(func() error {
			// Read the config map from the cluster.
			cm, err := c.Kube().CoreV1().ConfigMaps(ns).Get(context.TODO(), cmName, v1.GetOptions{})
			if err != nil {
				return err
			}

			// Get the MeshConfig yaml from the config map.
			mcYaml, ok := cm.Data["mesh"]
			if !ok {
				return fmt.Errorf("mesh config was missing in istio config map for %s", c.Name())
			}
			mu.Lock()
			origCfg[c.Name()] = cm.Data["mesh"]
			mu.Unlock()

			// Parse the YAML.
			mc := &meshconfig.MeshConfig{}
			if err := protomarshal.ApplyYAML(mcYaml, mc); err != nil {
				return err
			}

			// Apply the change.
			if err := update(mc); err != nil {
				return err
			}

			// Store the updated MeshConfig back into the config map.
			cm.Data["mesh"], err = protomarshal.ToYAML(mc)
			if err != nil {
				return err
			}

			// Write the config map back to the cluster.
			_, err = c.Kube().CoreV1().ConfigMaps(ns).Update(context.TODO(), cm, v1.UpdateOptions{})
			if err != nil {
				return err
			}
			scopes.Framework.Infof("patched %s meshconfig:\n%s", c.Name(), cm.Data["mesh"])
			return nil
		})
	}

	// Restore the original value of the MeshConfig when the context completes.
	t.CleanupStrategy(cleanupStrategy, func() {
		errG := multierror.Group{}
		mu.RLock()
		defer mu.RUnlock()
		for cn, mcYaml := range origCfg {
			cn, mcYaml := cn, mcYaml
			c := clusters.GetByName(cn)
			errG.Go(func() error {
				cm, err := c.Kube().CoreV1().ConfigMaps(ns).Get(context.TODO(), cmName, v1.GetOptions{})
				if err != nil {
					return err
				}
				cm.Data["mesh"] = mcYaml
				_, err = c.Kube().CoreV1().ConfigMaps(ns).Update(context.TODO(), cm, v1.UpdateOptions{})
				return err
			})
		}
		if err := errG.Wait().ErrorOrNil(); err != nil {
			scopes.Framework.Errorf("failed cleaning up cluster-local config: %v", err)
		}
	})
	return errG.Wait().ErrorOrNil()
}

func UpdateMeshConfigOrFail(t framework.TestContext, ns string, clusters cluster.Clusters,
	update func(*meshconfig.MeshConfig) error, cleanupStrategy cleanup.Strategy,
) {
	t.Helper()
	if err := UpdateMeshConfig(t, ns, clusters, update, cleanupStrategy); err != nil {
		t.Fatal(err)
	}
}

func PatchMeshConfig(t resource.Context, ns string, clusters cluster.Clusters, patch string) error {
	return UpdateMeshConfig(t, ns, clusters, func(mc *meshconfig.MeshConfig) error {
		return protomarshal.ApplyYAML(patch, mc)
	}, cleanup.Always)
}

func PatchMeshConfigOrFail(t framework.TestContext, ns string, clusters cluster.Clusters, patch string) {
	t.Helper()
	if err := PatchMeshConfig(t, ns, clusters, patch); err != nil {
		t.Fatal(err)
	}
}

// This regular expression matches list object index selection expression such as
// abc[100], Tba_a[0].
var listObjRex = regexp.MustCompile(`^([a-zA-Z]?[a-z_A-Z\d]*)\[([ ]*[\d]+)[ ]*\]$`)

func getConfigValue(path []string, val map[string]*structpb.Value) *structpb.Value {
	retVal := structpb.NewNullValue()
	if len(path) > 0 {
		match := listObjRex.FindStringSubmatch(path[0])
		// valid list index
		switch len(match) {
		case 0: // does not match list object selection, should be name of a field, should be struct value
			thisVal := val[path[0]]
			// If it is a struct and looking for more down the path
			if thisVal.GetStructValue() != nil && len(path) > 1 {
				return getConfigValue(path[1:], thisVal.GetStructValue().Fields)
			}
			retVal = thisVal
		case 3: // match somthing like aaa[100]
			thisVal := val[match[1]]
			// If it is a list and looking for more down the path
			if thisVal.GetListValue() != nil && len(path) > 1 {
				index, _ := strconv.Atoi(match[2])
				return getConfigValue(path[1:], thisVal.GetListValue().Values[index].GetStructValue().Fields)
			}
			retVal = thisVal
		}
	}
	return retVal
}

// This is method is to get a structpb value from a structpb map by
// using a dotted path such as `pilot.env.LOCAL_CLUSTER_SECRET_WATCHER`.
func GetConfigValue(path string, val map[string]*structpb.Value) *structpb.Value {
	return getConfigValue(strings.Split(path, "."), val)
}
