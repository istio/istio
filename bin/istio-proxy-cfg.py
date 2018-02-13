#!/usr/bin/env python

import os
import requests
import json
import subprocess
import collections
import argparse
import yaml
import sys

POD = collections.namedtuple('Pod', ['name', 'namespace', 'ip', 'labels'])


"""
    XDS fetches routing information from pilot.
    The information is in XDS V1 format.
"""

PILOT_SVC = "istio-pilot"
ISTIO_NS = "istio-system"
CLUSTER = "istio-proxy"

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
        print url
        try:
            if post:
                return requests.post(url, headers=self.headers).json()
            else:
                return requests.get(url, headers=self.headers).json()
        except Exception as ex:
            print ex
            print "Is pilot accessible at %s?" % url
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

    def cache_stats(self):
        return self.query("/cache_stats")

    def clear_cache_stats(self):
        return self.query("/clear_cache_stats", post=True)

# Class XDS end

# Proxy class
"""
    Proxy uses envoy admin port to fetch routing information.
    Proxy provides data in XDS V2 format.
"""


class Proxy(object):

    def __init__(self, pod):
        self.pod = pod

    def query(self, path):
        if not path.startswith("/"):
            path = "/" + path

        cname = "istio-proxy"
        if "ingress" in self.pod.name:
            cname = "istio-ingress"

        cmd = "kubectl -n {pod.namespace} exec -i -t {pod.name} -c {cname} -- curl http://localhost:15000{path}".format(
            pod=self.pod,
            path=path,
            cname=cname
        )
        print cmd
        # The result is returned as a list of json objects.
        # { "a": "b"
        # }
        # { "c": "d"
        # }
        # Note the lack of "," between objects
        # Convert it into an array of objects
        s = subprocess.check_output(cmd.split())
        s = s.replace('}\r\n{', '},\r\n{')
        s = s.replace('}\n{', '},\n{')
        return json.loads("[" + s + "]")

    def routes(self):
        return self.query("/routes")


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
        si = searchstr.split(',')
        if len(si) != 3:
            print "podname must be either name,namespace,podip or name.namespace or any string that's a pod's label or a prefix of a pod's name"
            return None

        podname = si[0]
        podns = si[1]
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


def find_pilot_url():
    try:
        pilot_svc = subprocess.check_output(
            "kubectl get svc {svc} -n {ns} -o json".format(svc=PILOT_SVC, ns=ISTIO_NS).split())
    except:
        pilot_svc = {}
    pilot_url = ""
    if pilot_svc:
        pilot_spec = json.loads(pilot_svc)['spec']
        for port in pilot_spec['ports']:
            if port['name'] == 'http-discovery':
                pilot_url = "http://{ip}:{port}".format(ip=pilot_spec['clusterIP'], port=port['port'])
                break
    return pilot_url


def main(args):
    pods = searchpod(pod_info(), args.podname)

    if not pods:
        print "Cound not find pod ", args.podname
        return -1

    if len(pods) > 1:
        podnames = ["%s.%s" % (pod.name, pod.namespace) for pod in pods]
        print "More than one pod is found: %s" % ", ".join(podnames)
        return -1

    pod = pods[0]
    pilot_url = args.pilot_url
    if not pilot_url:
        pilot_url = find_pilot_url()

    if args.output is None:
        output_dir = "./" + pod.name
    else:
        output_dir = args.output + "/" + pod.name

    try:
        os.makedirs(output_dir + "/" + pod.name)
    except OSError:
        if not os.path.isdir(output_dir):
            raise

    output_file = output_dir + "/" + "pilot_xds.yaml"
    op = open(output_file, "wt")
    print "Fetching from Pilot for pod %s in %s namespace" % (pod.name, pod.namespace)
    xds = XDS(url=pilot_url)
    data = xds.lds(pod, True)
    yaml.safe_dump(data, op, default_flow_style=False,
                   allow_unicode=False, indent=2)
    print "Wrote ", output_file

    output_file = output_dir + "/" + "proxy_xds.yaml"
    op = open(output_file, "wt")
    print("Fetching from Envoy for pod %s in %s namespace" % (pod.name, pod.namespace))
    pr = Proxy(pod)
    data = pr.routes()
    yaml.safe_dump(data, op, default_flow_style=False,
                   allow_unicode=False, indent=2)
    print "Wrote ", output_file

    if args.cache_stats:
        output_file = output_dir + "/" + "stats_xds.yaml"
        op = open(output_file, "wt")
        data = xds.cache_stats()
        print("Fetching Pilot cache stats")
        yaml.safe_dump(data, op, default_flow_style=False,
                       allow_unicode=False, indent=2)
        print "Wrote ", output_file

    if args.clear_cache_stats:
        xds.clear_cache_stats()

    return 0

if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Fetch routes from Envoy or Pilot for a given pod")

    parser.add_argument("--pilot_url",
                        help="Often this is localhost:8080 or 15003 through a port-forward."
                        " \n\nkubectl --namespace=istio-system port-forward $(kubectl --namespace=istio-system get -l istio=pilot pod -o=jsonpath='{.items[0].metadata.name}') 8080:8080."
                        "\n\nIf not provided, attempt will be made to find it out."
                        )
    parser.add_argument("podname", help="podname must be either name.namespace.podip or name.namespace or any string that is a pod's label or a prefix of a pod's name. ingress, mixer, istio-ca, product-page all work")
    parser.add_argument(
        "--output", help="A directory where output files are saved. default is the current directory")
    parser.add_argument(
        "--cache_stats", action='store_true', help="Fetch Pilot cache stats")
    parser.add_argument(
        "--clear_cache_stats", action='store_true', help="Clear Pilot cache stats")
    args = parser.parse_args()
    sys.exit(main(args))
