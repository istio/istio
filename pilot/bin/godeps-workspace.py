#!/usr/bin/python
# Script to process Godeps.json into WORKSPACE
# TODO: you need to manually remove duplicate entries if importpath is a directory in a repo

import json
import sys

obj = json.load(sys.stdin)
deps = obj['Deps']
for dep in deps:
    path = dep['ImportPath']
    commit = dep['Rev']

    # name: gopkg.in/inf.v0 -> in_gopkg_inf_v0
    # name: github.com/googleapis/gax-go -> com_github_googleapis_gax_go
    slash = path.index('/')
    dns = path[:slash].split('.')
    dns.reverse()
    name = '_'.join(dns) + "_" + path[slash+1:].replace('.', '_').replace('-', '_').replace('/', '_')

    print("new_go_repository(")
    print("    name = \"" + name + "\",")
    print("    commit = \"" + commit + "\",")
    print("    importpath = \"" + path + "\",")
    print(")")
    print("")
