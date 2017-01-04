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
    def new_go_repository(self, name, path):
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
            if not callable(fn):
                continue

            path, name = keywords(stmt)
            if path.endswith(".git"):
                path = path[:-4]
            path = pathmap.get(path, path)
            tup = fn(name, path)
            lst.append(tup)

    return lst


def makelink(target, linksrc):
    # make a symlink from vendor/path --> target
    try:
        os.makedirs(os.path.dirname(linksrc))
    except Exception as e1:
        if 'File exists:' not in str(e1):
            print type(e1), e1
    try:
        os.remove(linksrc)
    except Exception as e1:
        if 'No such file or directory' not in str(e1):
            print type(e1), e1
    os.symlink(target, linksrc)


def bazel_to_vendor(WKSPC):
    WKSPC = os.path.abspath(WKSPC)
    workspace = WKSPC + "/WORKSPACE"
    if not os.path.isfile(workspace):
        print "WORKSPACE file not found in " + WKSPC
        print "prog BAZEL_WORKSPACE_DIR"
        return -1
    lf = os.readlink(WKSPC + "/bazel-mixer")
    external = lf.replace("/execroot/mixer", "/external")
    vendor = WKSPC + "/vendor"
    genfiles = WKSPC + "/bazel-genfiles/external/"
    vlen = len(vendor)
    for (target, linksrc) in process(workspace, external, genfiles, vendor):
        makelink(target, linksrc)
        print "Vendored", linksrc[vlen + 1:]


def main(args):
    WKSPC = os.getcwd()
    if len(args) > 0:
        WKSPC = args[0]

    bazel_to_vendor(WKSPC)

if __name__ == "__main__":
    import sys
    sys.exit(main(sys.argv[1:]))
