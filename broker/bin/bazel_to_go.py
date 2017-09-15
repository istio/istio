#!/usr/bin/env python

#
# Makes a bazel workspace play nicely with standard go tools
# go build
# go test
# should work after this
#
# It does so by making symlinks from WORKSPACE/vendor to the bazel
# sandbox dirs
#
import glob
import os

import ast
from urlparse import urlparse

THIS_DIR = os.path.dirname(os.path.abspath(__file__))

KEYS = set(["importpath", "remote", "name"])


def keywords(stmt):
    kw = {k.arg: k.value.s for k in stmt.keywords if k.arg in KEYS}
    path = kw.get("importpath", kw.get("remote"))

    u = urlparse(path)
    return u.netloc + u.path, kw["name"]

pathmap = {
    "github.com/istio/api": "istio.io/api"
}

known_repos = {
        "org_golang_google": "google.golang.org",
        "com_github": "github.com",
        "org_golang": "golang.org",
        "in_gopkg": "gopkg.in"
}


# gopkg packages are of type gopkg.in/yaml.v2
# in_gopkg_yaml_v2
# com_github_hashicorp_go_multierror  --> github.com/
def repos(name):
   for r, m in known_repos.items():
       if name.startswith(r):
           rname = name[(len(r)+1):]
           fp, _, rest = rname.partition('_')
           if r == 'in_gopkg':
               return m + "/" + fp + "." + rest

           return m + "/" + fp + "/" + rest

# If we need to support more bazel functions
# add them here


class WORKSPACE(object):

    def __init__(self, external, genfiles, vendor):
        self.external = external
        self.genfiles = genfiles
        self.vendor = vendor

    # All functions should return a tuple
    # link target, source
    # target should exist
    def go_repository(self, name, path):
        return ((self.external + "/" + name, self.vendor + "/" + path))

    def new_git_repository(self, name, path):
        return((self.genfiles + "/" + name, self.vendor + "/" + path))

    def new_git_or_local_repository(self, name, path):
        return self.new_git_repository(name, path)


def process(fl, external, genfiles, vendor):
    src = open(fl).read()
    tree = ast.parse(src, fl)
    lst = []
    wksp = WORKSPACE(external, genfiles, vendor)

    for stmt in ast.walk(tree):
        stmttype = type(stmt)
        if stmttype == ast.Call:

            fn = getattr(wksp, stmt.func.id, "")
