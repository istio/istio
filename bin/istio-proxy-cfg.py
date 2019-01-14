#!/usr/bin/env python
#
# Copyright 2018 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from __future__ import print_function
import os
import sys

import argparse
import collections
import json
import logging
import random
import requests
import subprocess
import yaml
import time

logging.basicConfig(format="%(message)s")

POD = collections.namedtuple('Pod', ['name', 'namespace', 'ip', 'labels'])


"""
    XDS fetches routing information from pilot.
    The information is in XDS V1 format.
"""

PILOT_SVC = "istio-pilot"
ISTIO_NS = "istio-system"
CLUSTER = "istio-proxy"
ENVOY_PORT = 15000
LOCAL_PORT_START = 50000
LOCAL_PORT_STOP = 60000


class XDS(object):

    def __init__(self, url, ns=ISTIO_NS, cluster=CLUSTER, headers=None):
        self.url = url
        self.ns = ns
        self.cluster = cluster
        self.headers = headers
        self.cds_info = {}
        self.sds_info = {}

    def key(self, pod):
        role = "sidecar"
        if "ingress" in pod.name:
            role = "ingress"
        elif "egress" in pod.name:
            role = "egress"

        return "{role}~{pod.ip}~{pod.name}.{pod.namespace}~{pod.namespace}.svc.cluster.local".format(
            role=role, pod=pod)

    def query(self, path, post=False):
        url = self.url + path
        logging.info(url)
        try:
            if post:
                return requests.post(url, headers=self.headers)
            else:
                return requests.get(url, headers=self.headers).json()
        except Exception as ex:
            logging.error(ex)
            logging.error("Is pilot accessible at %s?" % url)
            sys.exit(-1)

    def lds(self, pod, hydrate=False):
        data = self.query("/v1/listeners/{cluster}/{key}".format(
            cluster=self.cluster, key=self.key(pod)))
        if not hydrate:
            return data

        # call rds

        for l in data['listeners']:
            for f in l['filters']:
                if 'config' not in f:
                    continue
                if 'rds' not in f['config']:
                    continue

                if 'route_config_name' not in f['config']['rds']:
                    continue
                rn = f['config']['rds']['route_config_name']

                # found a route fetch it
                f['config']['route_config'] = self.rds(pod, rn, hydrate)
                f['config']['route_config']['name'] = rn

        data["clusters"] = self.cds_info
        return data

    def rds(self, pod, route="80", hydrate=False):
        data = self.query("/v1/routes/{route}/{cluster}/{key}".format(
            route=route, cluster=self.cluster, key=self.key(pod)))
        if not hydrate:
            return data

        # check if we should hydrate cds
        for vh in data['virtual_hosts']:
            for route in vh['routes']:
                if 'cluster' in route:
                    cn = route['cluster']
                    route['cluster'] = self.cds(pod, cn, hydrate)
                elif 'weighted_clusters' in route:
                    for cls in route['weighted_clusters']['clusters']:
                        cn = cls['name']
                        cls['cluster'] = self.cds(pod, cn, hydrate)
        return data

    def cds(self, pod, cn, hydrate=False):
        pk = self.key(pod)
        if pk not in self.cds_info:
            data = self.query("/v1/clusters/{cluster}/{key}".format(
                cluster=self.cluster, key=self.key(pod)))
            self.cds_info[pk] = {c['name']: c for c in data['clusters']}

            if hydrate:
                for sn, cl in self.cds_info[pk].items():
                    if cl['type'] != "sds":
                        continue
                    cl['endpoints'] = self.sds(cl['service_name'])

        return self.cds_info[pk][cn]

    def sds(self, service_key):
        if service_key not in self.sds_info:
            self.sds_info[service_key] = self.query(
                "/v1/registration/{service_key}".format(service_key=service_key))
        return self.sds_info[service_key]

# Class XDS end

# Proxy class
"""
    Proxy uses envoy admin port to fetch routing information.
    Proxy provides data in XDS V2 format.
"""


class Proxy(object):

    def __init__(self, envoy_url):
        self.envoy_url = envoy_url

    def query(self, path, use_json=True):
        url = self.envoy_url + path
        logging.info(url)
        try:
            s = requests.post(url)
        except Exception as ex:
            logging.error(ex)
            logging.error("Is envoy accessible at %s?" % url)
            sys.exit(-1)
        if use_json:
            return s.json()
        else:
            return s.text

    def routes(self):
        return self.query("/routes")

    def clusters(self):
        return self.query("/clusters", use_json=False)

    def listeners(self):
        return self.query("/listeners")


def pod_info():
    op = subprocess.check_output(
        "kubectl get pod --all-namespaces -o json".split())
    o = json.loads(op)
    return {i['metadata']['name']:
            POD(i['metadata']['name'], i['metadata']['namespace'],
                i['status']['podIP'], i['metadata']['labels']) for i in o['items']}


def searchpod(pi, searchstr):
    podname = podns = podip = ""
    if "." in searchstr:
        si = searchstr.split('.')
        if len(si) != 3 and len(si) != 2:
            logging.warning(
                "podname must be either name.namespace.podip or name.namespace or any string that's a pod's label or a prefix of a pod's name")
            return None

        podname = si[0]
        podns = si[1]
        if len(si) == 3:
            podip = si[2]

    pods = []
    for pn, pod in pi.items():
        if podname and podns:
            if podip:
                if podname == pod.name and podns == pod.namespace and podip == pod.ip:
                    pods.append(pod)
                    return pods
            elif podname == pod.name and podns == pod.namespace:
                pods.append(pod)
                return pods
        elif searchstr in pod.labels.values():
            pods.append(pod)
        elif pn.startswith(searchstr):
            pods.append(pod)

    return pods


def start_port_forward(pod_name, namespace, remote_port):
    local_port = random.randrange(LOCAL_PORT_START, LOCAL_PORT_STOP)
    port_forward_pid = ""
    url = ""
    try:
        port_forward_pid = subprocess.Popen("kubectl --namespace={namespace} port-forward {pod_name} {local_port}:{remote_port}".format(
            pod_name=pod_name, namespace=namespace, local_port=local_port, remote_port=remote_port).split(), stdout=open(os.devnull, "wb")).pid
    except:
        logging.error("Failed to create port-forward for pod %s.%s with remote port %s" %
                      (pod_name, namespace, remote_port))
        raise
    else:
        url = "http://localhost:{port}".format(port=local_port)
        # wait until the port-forward process is fully up
        while True:
            try:
                requests.get(url)
            except:
                time.sleep(.1)
            else:
                break
    return url, port_forward_pid


def find_pilot_url():
    try:
        pilot_svc = subprocess.check_output(
            "kubectl get svc {svc} -n {ns} -o json".format(svc=PILOT_SVC, ns=ISTIO_NS).split())
    except:
        pilot_svc = {}
    pilot_url = ""
    pilot_port = ""
    port_forward_pid = ""
    if pilot_svc:
        pilot_spec = json.loads(pilot_svc)['spec']
        discovery_port = ""
        legacy_discovery_port = ""
        for port in pilot_spec['ports']:
            if port['name'] == 'http-legacy-discovery':
                legacy_discovery_port = port['port']
            elif port['name'] == 'http-discovery':
                discovery_port = port['port']
        if legacy_discovery_port:
            pilot_port = legacy_discovery_port
        else:
            pilot_port = discovery_port
        pilot_url = "http://{ip}:{port}".format(
            ip=pilot_spec['clusterIP'], port=pilot_port)

        try:
            requests.get(pilot_url, timeout=2)
        except:
            logging.warning(
                "It seems that you are running outside the k8s cluster")
            logging.warning(
                "Let's try to create a port-forward to access pilot")
            cmd = "kubectl --namespace=%s get -l istio=pilot pod -o=jsonpath={.items[0].metadata.name}" % (
                ISTIO_NS)
            pod_name = subprocess.check_output(cmd.split())
            pilot_url, port_forward_pid = start_port_forward(
                pod_name, ISTIO_NS, pilot_port)

    return pilot_url, port_forward_pid


def main(args):
    pods = searchpod(pod_info(), args.podname)

    if not pods:
        logging.error("Cound not find pod %s" % args.podname)
        return -1

    if len(pods) > 1:
        podnames = ["%s.%s" % (pod.name, pod.namespace) for pod in pods]
        logging.error("More than one pod is found: %s" % ", ".join(podnames))
        return -1

    pod = pods[0]

    if args.output is None:
        output_dir = "/tmp/" + pod.name
    else:
        output_dir = args.output + "/" + pod.name

    try:
        os.makedirs(output_dir)
    except OSError:
        if not os.path.isdir(output_dir):
            raise

    if not args.skip_pilot:
        pilot_url = args.pilot_url
        pilot_port_forward_pid = ""
        if pilot_url:
            if not pilot_url.startswith("http://") and not pilot_url.startswith("https://"):
                pilot_url = "http://" + pilot_url
        else:
            pilot_url, pilot_port_forward_pid = find_pilot_url()

        output_file = output_dir + "/" + "pilot_xds.yaml"
        op = open(output_file, "wt")
        logging.info("Fetching from Pilot for pod %s in %s namespace" %
                     (pod.name, pod.namespace))
        xds = XDS(url=pilot_url)
        data = xds.lds(pod, True)
        yaml.safe_dump(data, op, default_flow_style=False,
                       allow_unicode=False, indent=2)
        print("Wrote ", output_file)

        if pilot_port_forward_pid:
            subprocess.call(["kill", "%s" % pilot_port_forward_pid])

    if not args.skip_envoy:
        envoy_url, envoy_port_forward_pid = start_port_forward(
            pod.name, pod.namespace, ENVOY_PORT)
        logging.info("Fetching from Envoy for pod %s in %s namespace" %
                     (pod.name, pod.namespace))
        pr = Proxy(envoy_url)
        output_file = output_dir + "/" + "proxy_routes.yaml"
        op = open(output_file, "wt")
        data = pr.routes()
        yaml.safe_dump(data, op, default_flow_style=False,
                       allow_unicode=False, indent=2)
        print("Wrote ", output_file)

        output_file = output_dir + "/" + "proxy_listeners.yaml"
        op = open(output_file, "wt")
        data = pr.listeners()
        yaml.safe_dump(data, op, default_flow_style=False,
                       allow_unicode=False, indent=2)
        print("Wrote ", output_file)

        output_file = output_dir + "/" + "proxy_clusters.yaml"
        op = open(output_file, "wt")
        data = pr.clusters()
        op.write(data)
        print("Wrote ", output_file)

        if envoy_port_forward_pid:
            subprocess.call(["kill", "%s" % envoy_port_forward_pid])

    return 0

if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Fetch routes from Envoy or Pilot for a given pod")

    parser.add_argument("--pilot_url",
                        help="Often this is localhost:8080 or 15005 (https) or 15007 (http) through a port-forward."
                        " \n\nkubectl --namespace=istio-system port-forward $(kubectl --namespace=istio-system get -l istio=pilot pod -o=jsonpath='{.items[0].metadata.name}') 8080:8080."
                        "\n\nIf not provided, attempt will be made to find it out."
                        )
    parser.add_argument("podname", help="podname must be either name.namespace.podip or name.namespace or any string that is a pod's label or a prefix of a pod's name. ingress, mixer, citadel, product-page all work")
    parser.add_argument(
        "--output", help="A directory where output files are saved. default is the /tmp directory")
    parser.add_argument(
        "--skip_envoy", action='store_true', help="Fetch Envoy configuration from a pod")
    parser.add_argument(
        "--skip_pilot", action='store_true', help="Fetch from pilot Proxy configuration for a pod")
    parser.add_argument(
        "--show_ssl_summary",
        action="store_true",
        help="If set, show summary for ssl context for listeners that have it")
    args = parser.parse_args()
    sys.exit(main(args))
