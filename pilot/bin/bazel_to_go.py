#!/usr/bin/env python

import glob
import os
import ast
from urlparse import urlparse

def process(fl, external, vendor):
    src = open(fl).read()
    tree = ast.parse(src, fl)
    links = {}

    for stmt in ast.walk(tree):
        stmttype = type(stmt)
        if stmttype == ast.Call and stmt.func.id == "new_go_repository":
            kw = {k.arg: k.value.s for k in stmt.keywords if k.arg in ["name", "importpath"]}
            source = external + "/" + kw["name"]
            u = urlparse(kw["importpath"])
            target = vendor + "/" + u.netloc + u.path
            links[source] = target

    return links

def makelink(source, name):
    print "makelink", name, "from", source

    try:
        os.makedirs(os.path.dirname(name))
    except Exception as e1:
        if 'File exists:' not in str(e1):
            print type(e1), e1
    try:
        os.remove(name)
    except Exception as e1:
        if 'Is a directory' in str(e1):
            return
        if 'No such file or directory' not in str(e1):
            print type(e1), e1

    if not os.path.exists(source):
        print source, "Does not exist"
        return

    os.symlink(source, name)

def bazel_to_vendor(WKSPC):
    WKSPC = os.path.abspath(WKSPC)
    workspace = WKSPC + "/WORKSPACE"
    vendor = WKSPC + "/vendor"
    root = WKSPC + "/bazel-manager"

    if not os.path.isfile(workspace):
        print "WORKSPACE file not found in " + WKSPC
        print "prog BAZEL_WORKSPACE_DIR"
        return -1

    # resolve symlink to bazel workspace
    lf = os.readlink(WKSPC + "/bazel-manager")
    EXEC_ROOT = os.path.dirname(lf)
    BLD_DIR = os.path.dirname(EXEC_ROOT)
    external =  BLD_DIR + "/external"

    links = process(workspace, external, vendor)
    for (source, target) in links.items():
        makelink(source, target)

def main(args):
    WKSPC = os.getcwd()
    if len(args) > 0:
        WKSPC = args[0]
    bazel_to_vendor(WKSPC)

if __name__ == "__main__":
    import sys
    sys.exit(main(sys.argv[1:]))
