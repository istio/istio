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

package ambient

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	pconstants "istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube/kclient"
	istiolog "istio.io/pkg/log"
)

var log = istiolog.RegisterScope("ambient", "ambient controller", 0)

func RouteExists(rte []string) bool {
	output, err := executeOutput(
		"bash", "-c",
		fmt.Sprintf("ip route show %s | wc -l", strings.Join(rte, " ")),
	)
	if err != nil {
		return false
	}

	log.Debugf("RouteExists(%s): %s", strings.Join(rte, " "), output)

	return output == "1"
}

func AddPodToMesh(client kubernetes.Interface, pod *corev1.Pod, ip string) {
	addPodToMeshWithIptables(pod, ip)

	if err := AnnotateEnrolledPod(client, pod); err != nil {
		log.Errorf("failed to annotate pod enrollment: %v", err)
	}
}

func addPodToMeshWithIptables(pod *corev1.Pod, ip string) {
	if ip == "" {
		ip = pod.Status.PodIP
	}
	if ip == "" {
		log.Debugf("skip adding pod %s/%s, IP not yet allocated", pod.Name, pod.Namespace)
		return
	}

	if !IsPodInIpset(pod) {
		log.Infof("Adding pod '%s/%s' (%s) to ipset", pod.Name, pod.Namespace, string(pod.UID))
		err := Ipset.AddIP(net.ParseIP(ip).To4(), string(pod.UID))
		if err != nil {
			log.Errorf("Failed to add pod %s to ipset list: %v", pod.Name, err)
		}
	} else {
		log.Infof("Pod '%s/%s' (%s) is in ipset", pod.Name, pod.Namespace, string(pod.UID))
	}

	rte, err := buildRouteFromPod(pod, ip)
	if err != nil {
		log.Errorf("Failed to build route for pod %s: %v", pod.Name, err)
	}

	if !RouteExists(rte) {
		log.Infof("Adding route for %s/%s: %+v", pod.Name, pod.Namespace, rte)
		// @TODO Try and figure out why buildRouteFromPod doesn't return a good route that we can
		// use err = netlink.RouteAdd(rte):
		// Error: {"level":"error","time":"2022-06-24T16:30:59.083809Z","msg":"Failed to add route ({Ifindex: 4 Dst: 10.244.2.7/32
		// Via: Family: 2, Address: 192.168.126.2 Src: 10.244.2.1 Gw: <nil> Flags: [] Table: 100 Realm: 0}) for pod
		// helloworld-v2-same-node-67b6b764bf-zhmp4: invalid argument"}
		err = execute("ip", append([]string{"route", "add"}, rte...)...)
		if err != nil {
			log.Warnf("Failed to add route (%s) for pod %s: %v", rte, pod.Name, err)
		}
	} else {
		log.Infof("Route already exists for %s/%s: %+v", pod.Name, pod.Namespace, rte)
	}

	dev, err := getDeviceWithDestinationOf(ip)
	if err != nil {
		log.Warnf("Failed to get device for destination %s", ip)
		return
	}

	err = disableRPFiltersForLink(dev)
	if err != nil {
		log.Warnf("failed to disable procfs rp_filter for device %s: %v", dev, err)
	}
}

var annotationPatch = []byte(fmt.Sprintf(
	`{"metadata":{"annotations":{"%s":"%s"}}}`,
	pconstants.AmbientRedirection,
	pconstants.AmbientRedirectionEnabled,
))

var annotationRemovePatch = []byte(fmt.Sprintf(
	`{"metadata":{"annotations":{"%s":null}}}`,
	pconstants.AmbientRedirection,
))

func AnnotateEnrolledPod(client kubernetes.Interface, pod *corev1.Pod) error {
	_, err := client.CoreV1().
		Pods(pod.Namespace).
		Patch(
			context.Background(),
			pod.Name,
			types.MergePatchType,
			annotationPatch,
			metav1.PatchOptions{},
		)
	return err
}

func AnnotateUnenrollPod(client kubernetes.Interface, pod *corev1.Pod) error {
	if pod.Annotations[pconstants.AmbientRedirection] != pconstants.AmbientRedirectionEnabled {
		return nil
	}
	// TODO: do not overwrite if already none
	_, err := client.CoreV1().
		Pods(pod.Namespace).
		Patch(
			context.Background(),
			pod.Name,
			types.MergePatchType,
			annotationRemovePatch,
			metav1.PatchOptions{},
		)
	return err
}

func DelPodFromMesh(client kubernetes.Interface, pod *corev1.Pod) {
	log.Debugf("Removing pod '%s/%s' (%s) from mesh", pod.Name, pod.Namespace, string(pod.UID))
	if IsPodInIpset(pod) {
		log.Infof("Removing pod '%s' (%s) from ipset", pod.Name, string(pod.UID))
		err := Ipset.DeleteIP(net.ParseIP(pod.Status.PodIP).To4())
		if err != nil {
			log.Errorf("Failed to delete pod %s from ipset list: %v", pod.Name, err)
		}
	} else {
		log.Infof("Pod '%s/%s' (%s) is not in ipset", pod.Name, pod.Namespace, string(pod.UID))
	}
	rte, err := buildRouteFromPod(pod, "")
	if err != nil {
		log.Errorf("Failed to build route for pod %s: %v", pod.Name, err)
	}
	if RouteExists(rte) {
		log.Infof("Removing route: %+v", rte)
		// @TODO Try and figure out why buildRouteFromPod doesn't return a good route that we can
		// use this:
		// err = netlink.RouteDel(rte)
		err = execute("ip", append([]string{"route", "del"}, rte...)...)
		if err != nil {
			log.Warnf("Failed to delete route (%s) for pod %s: %v", rte, pod.Name, err)
		}
	}

	if err := AnnotateUnenrollPod(client, pod); err != nil {
		log.Errorf("failed to annotate pod unenrollment: %v", err)
	}
}

// GetHostIPByRoute get the automatically chosen host ip to the Pod's CIDR
func GetHostIPByRoute(pods kclient.Client[*corev1.Pod]) (string, error) {
	// We assume per node POD's CIDR is the same block, so the route to the POD
	// from host should be "same". Otherwise, there may multiple host IPs will be
	// used as source to dial to PODs.
	for _, pod := range pods.List(metav1.NamespaceAll, ztunnelLabels) {
		targetIP := pod.Status.PodIP
		if hostIP := getOutboundIP(targetIP); hostIP != nil {
			return hostIP.String(), nil
		}
	}
	return "", fmt.Errorf("failed to get outbound IP to Pods")
}

// Get preferred outbound ip of this machine
func getOutboundIP(ip string) net.IP {
	conn, err := net.Dial("udp", ip+":80")
	if err != nil {
		return nil
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP
}

func GetHostIP(kubeClient kubernetes.Interface) (string, error) {
	var ip string
	// Get the node from the Kubernetes API
	node, err := kubeClient.CoreV1().Nodes().Get(context.TODO(), NodeName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("error getting node: %v", err)
	}

	ip = node.Spec.PodCIDR

	// This needs to be done as in Kind, the node internal IP is not the one we want.
	if ip == "" {
		// PodCIDR is not set, try to get the IP from the node internal IP
		for _, address := range node.Status.Addresses {
			if address.Type == corev1.NodeInternalIP {
				return address.Address, nil
			}
		}
	} else {
		network, err := netip.ParsePrefix(ip)
		if err != nil {
			return "", fmt.Errorf("error parsing node IP: %v", err)
		}

		ifaces, err := net.Interfaces()
		if err != nil {
			return "", fmt.Errorf("error getting interfaces: %v", err)
		}

		for _, iface := range ifaces {
			addrs, err := iface.Addrs()
			if err != nil {
				return "", fmt.Errorf("error getting addresses: %v", err)
			}

			for _, addr := range addrs {
				a, err := netip.ParseAddr(strings.Split(addr.String(), "/")[0])
				if err != nil {
					return "", fmt.Errorf("error parsing address: %v", err)
				}
				if network.Contains(a) {
					return a.String(), nil
				}
			}
		}
	}
	// fall back to use Node Internal IP
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			return address.Address, nil
		}
	}
	return "", nil
}

func (s *Server) AddPodToMesh(pod *corev1.Pod) {
	switch s.redirectMode {
	case IptablesMode:
		AddPodToMesh(s.kubeClient.Kube(), pod, "")
	case EbpfMode:
		if err := s.updatePodEbpfOnNode(pod); err != nil {
			log.Errorf("failed to update POD ebpf: %v", err)
		}
		if err := AnnotateEnrolledPod(s.kubeClient.Kube(), pod); err != nil {
			log.Errorf("failed to annotate pod enrollment: %v", err)
		}
	}
}

func (s *Server) DelPodFromMesh(pod *corev1.Pod) {
	switch s.redirectMode {
	case IptablesMode:
		DelPodFromMesh(s.kubeClient.Kube(), pod)
	case EbpfMode:
		if pod.Spec.HostNetwork {
			log.Debugf("pod(%s/%s) is using host network, skip it", pod.Namespace, pod.Name)
			return
		}
		if err := s.delPodEbpfOnNode(pod.Status.PodIP); err != nil {
			log.Errorf("failed to del POD ebpf: %v", err)
		}
		if err := AnnotateUnenrollPod(s.kubeClient.Kube(), pod); err != nil {
			log.Errorf("failed to annotate pod unenrollment: %v", err)
		}
	}
}

func SetProc(path string, value string) error {
	return os.WriteFile(path, []byte(value), 0o644)
}
