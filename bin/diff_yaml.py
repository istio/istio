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
#
# Compare 2 multi document kubernetes yaml files
# It ensures that order does not matter
#
from __future__ import print_function
import argparse
import datadiff
import sys
import yaml  # pyyaml

# returns fully qualified resource name of the k8s resource


def by_resource_name(res):
    if res is None:
        return ""

    return "{}::{}::{}".format(res['apiVersion'],
                               res['kind'],
                               res['metadata']['name'])


def keydiff(k0, k1):
    k0s = set(k0)
    k1s = set(k1)
    added = k1s - k0s
    removed = k0s - k1s
    common = k0s.intersection(k1s)

    return added, removed, common


def drop_keys(res, k1, k2):
    if k2 in res[k1]:
        del res[k1][k2]


def normalize_configmap(res):
    try:
        if res['kind'] != "ConfigMap":
            return res

        data = res['data']

        # some times keys are yamls...
        # so parse them
        for k in data:
            try:
                op = yaml.safe_load_all(data[k])
                data[k] = list(op)
            except yaml.YAMLError as ex:
                print(ex)

        return res
    except KeyError as ke:
        if 'kind' in str(ke) or 'data' in str(ke):
            return res

        raise


def normalize_ports(res):
    try:
        spec = res["spec"]
        if spec is None:
            return res
        ports = sorted(spec['ports'], key=lambda x: x["port"])
        spec['ports'] = ports

        return res
    except KeyError as ke:
        if 'spec' in str(ke) or 'ports' in str(ke) or 'port' in str(ke):
            return res

        raise


def normalize_res(res, args):
    if not res:
        return res

    if args.ignore_labels:
        drop_keys(res, "metadata", "labels")

    if args.ignore_namespace:
        drop_keys(res, "metadata", "namespace")

    res = normalize_ports(res)

    res = normalize_configmap(res)

    return res


def normalize(rl, args):
    for i in range(len(rl)):
        rl[i] = normalize_res(rl[i], args)

    return rl


def compare(args):
    j0 = normalize(list(yaml.safe_load_all(open(args.orig))), args)
    j1 = normalize(list(yaml.safe_load_all(open(args.new))), args)

    q0 = {by_resource_name(res): res for res in j0 if res is not None}
    q1 = {by_resource_name(res): res for res in j1 if res is not None}

    added, removed, common = keydiff(q0.keys(), q1.keys())

    changed = 0
    for k in sorted(common):
        if q0[k] != q1[k]:
            changed += 1

    print("## +++ ", args.new)
    print("## --- ", args.orig)
    print("## Added:", len(added))
    print("## Removed:", len(removed))
    print("## Updated:", changed)
    print("## Unchanged:", len(common) - changed)

    for k in sorted(added):
        print("+", k)

    for k in sorted(removed):
        print("-", k)

    print("##", "*" * 25)

    for k in sorted(common):
        if q0[k] != q1[k]:
            print("## ", k)
            s0 = yaml.safe_dump(q0[k], default_flow_style=False, indent=2)
            s1 = yaml.safe_dump(q1[k], default_flow_style=False, indent=2)

            print(datadiff.diff(s0, s1, fromfile=args.orig, tofile=args.new))

    return changed + len(added) + len(removed)


def main(args):
    return compare(args)


def get_parser():
    parser = argparse.ArgumentParser(
        description="Compare kubernetes yaml files")

    parser.add_argument("orig")
    parser.add_argument("new")
    parser.add_argument("--ignore-namespace", action="store_true", default=False,
                        help="Ignore namespace during comparison")
    parser.add_argument("--ignore-labels", action="store_true", default=False,
                        help="Ignore resource labels during comparison")
    parser.add_argument("--ignore-annotations", action="store_true", default=False,
                        help="Ignore annotations during comparison")

    return parser


if __name__ == "__main__":
    parser = get_parser()
    args = parser.parse_args()
    sys.exit(main(args))
