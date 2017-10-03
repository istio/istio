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
import subprocess

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
    src = subprocess.Popen("bazel query 'kind(\"go_repository|new_git.*_repository\", \"//external:*\")' --output=build", shell=True, stdout=subprocess.PIPE).stdout.read()
    #print src
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
        if 'Is a directory' in str(e1):
            return
        if 'No such file or directory' not in str(e1):
            print type(e1), e1
    if not os.path.exists(target):
        print target, "Does not exist"
        return

    # resolve target if it is a symlink
    realpath = os.path.realpath(target)

    os.symlink(realpath, linksrc)
#    print "Linked ", linksrc, '-->', target

def bazel_to_vendor(WKSPC):
    WKSPC = os.path.abspath(WKSPC)
    workspace = WKSPC + "/WORKSPACE"
    if not os.path.isfile(workspace):
        print "WORKSPACE file not found in " + WKSPC
        print "prog BAZEL_WORKSPACE_DIR"
        return -1
    lf = os.readlink(WKSPC + "/bazel-mixer")
    EXEC_ROOT = os.path.dirname(lf)
    BLD_DIR = os.path.dirname(EXEC_ROOT)
    external =  BLD_DIR + "/external"
    vendor = WKSPC + "/vendor"
    genfiles = WKSPC + "/bazel-genfiles/external/"

    links = {target: linksrc for(target, linksrc) in process(workspace, external, genfiles, vendor)}

    bysrc = {}

    for (target, linksrc) in links.items():
        if linksrc.endswith("istio_mixer"):
            # skip mixer as an external repo to avoid
            # never ending loop
            continue

        makelink(target, linksrc)
        #print "Vendored", linksrc, '-->', target
        bysrc[linksrc] = target

    # check other directories in external
    # and symlink ones that were not covered thru workspace
    for ext_target in get_external_links(external):
        if ext_target.endswith("istio_mixer"):
            # skip mixer as an external repo to avoid
            # never ending loop
            continue

        target = external + "/" + ext_target
        if target in links:
            continue
        link = repos(ext_target)
        if not link:
            # print "Could not resolve", ext_target
            continue
        if link in pathmap:
            # skip remapped deps
            continue
        # print ext_target, link
        linksrc = vendor + "/" + link

        # only make this link if we have not made it above
        if linksrc in bysrc:
            # print "Skipping ", link
            continue

        makelink(target, linksrc)
        # print "Vendored", linksrc, '-->', target

    adapter_protos(WKSPC)
    aspect_protos(WKSPC)
    template_protos(WKSPC)
    tools_protos(WKSPC)
    tools_generated_files(WKSPC)
    config_proto(WKSPC, genfiles)
    attributes_list(WKSPC, genfiles)
    inventory(WKSPC)

def get_external_links(external):
    return [file for file in os.listdir(external) if os.path.isdir(external+"/"+file)]

def main(args):
    WKSPC = os.getcwd()
    if len(args) > 0:
        WKSPC = args[0]

    bazel_to_vendor(WKSPC)

def adapter_protos(WKSPC):
    for adapter in os.listdir(WKSPC + "/bazel-genfiles/adapter/"):
        if os.path.exists(WKSPC + "/bazel-genfiles/adapter/"+ adapter + "/config"):
            for file in os.listdir(WKSPC + "/bazel-genfiles/adapter/" + adapter + "/config"):
                if file.endswith(".pb.go"):
                    makelink(WKSPC + "/bazel-genfiles/adapter/"+ adapter + "/config/" + file, WKSPC + "/adapter/" +adapter + "/config/" + file)

# link pb.go files 2 levels down the dir
def template_protos(WKSPC):
    for file in os.listdir(WKSPC + "/bazel-genfiles/pkg/adapter/template"):
        if file.endswith(".pb.go"):
            makelink(WKSPC + "/bazel-genfiles/pkg/adapter/template/" + file, WKSPC + "/pkg/adapter/template/" + file)
    for template in os.listdir(WKSPC + "/bazel-genfiles/template"):
        if template.endswith(".gen.go"):
            makelink(WKSPC + "/bazel-genfiles/template/" + template, WKSPC + "/template/" + template)
        if os.path.isdir(WKSPC + "/bazel-genfiles/template/" + template):
            for file in os.listdir(WKSPC + "/bazel-genfiles/template/" + template):
                # check if there are files under /template/<some template dir>
                if file.endswith("_tmpl.pb.go") or file.endswith("handler.gen.go") or file.endswith("template.gen.go"):
                    makelink(WKSPC + "/bazel-genfiles/template/" + template + "/" + file, WKSPC + "/template/" +template + "/" + file)
                if os.path.isdir(WKSPC + "/bazel-genfiles/template/" + template + "/" + file):
                    for file2 in os.listdir(WKSPC + "/bazel-genfiles/template/" + template + "/" + file):
                        # check if there are files under /template/<some uber dir>/<some template dir>
                        if file2.endswith("_tmpl.pb.go") or file2.endswith("handler.gen.go"):
                            makelink(WKSPC + "/bazel-genfiles/template/" + template + "/" + file + "/" + file2, WKSPC + "/template/" + template + "/" + file + "/" + file2)
    for template in os.listdir(WKSPC + "/bazel-genfiles/test/template"):
        if template.endswith(".gen.go"):
            makelink(WKSPC + "/bazel-genfiles/test/template/" + template, WKSPC + "/test/template/" + template)
        if os.path.isdir(WKSPC + "/bazel-genfiles/test/template/" + template):
            for file in os.listdir(WKSPC + "/bazel-genfiles/test/template/" + template):
                # check if there are files under /template/<some template dir>
                if file.endswith("_tmpl.pb.go") or file.endswith("handler.gen.go"):
                    makelink(WKSPC + "/bazel-genfiles/test/template/" + template + "/" + file, WKSPC + "/test/template/" +template + "/" + file)

def aspect_protos(WKSPC):
    for aspect in os.listdir(WKSPC + "/bazel-genfiles/pkg/aspect/"):
        for file in os.listdir(WKSPC + "/bazel-genfiles/pkg/aspect/config"):
            if file.endswith(".pb.go"):
                makelink(WKSPC + "/bazel-genfiles/pkg/aspect/config/" + file, WKSPC + "/pkg/aspect/config/" + file)

def tools_protos(WKSPC):
    if os.path.exists(WKSPC + "/bazel-genfiles/pkg/adapter/template/"):
        for file in os.listdir(WKSPC + "/bazel-genfiles/pkg/adapter/template/"):
            if file.endswith(".pb.go"):
                makelink(WKSPC + "/bazel-genfiles/pkg/adapter/template/" + file, WKSPC + "/pkg/adapter/template/" + file)

def tools_generated_files(WKSPC):
    if os.path.exists(WKSPC + "/bazel-genfiles/tools/codegen/pkg/interfacegen/testdata"):
        for file in os.listdir(WKSPC + "/bazel-genfiles/tools/codegen/pkg/interfacegen/testdata"):
            if file.endswith("_proto.descriptor_set") or file.endswith("error_template.descriptor_set"):
                makelink(WKSPC + "/bazel-genfiles/tools/codegen/pkg/interfacegen/testdata/" + file, WKSPC + "/tools/codegen/pkg/interfacegen/testdata/" + file)
    if os.path.exists(WKSPC + "/bazel-genfiles/tools/codegen/pkg/bootstrapgen/testdata"):
        for file in os.listdir(WKSPC + "/bazel-genfiles/tools/codegen/pkg/bootstrapgen/testdata"):
            if file.endswith("_proto.descriptor_set"):
                makelink(WKSPC + "/bazel-genfiles/tools/codegen/pkg/bootstrapgen/testdata/" + file, WKSPC + "/tools/codegen/pkg/bootstrapgen/testdata/" + file)
    if os.path.exists(WKSPC + "/bazel-genfiles/tools/codegen/pkg/modelgen/testdata"):
            for file in os.listdir(WKSPC + "/bazel-genfiles/tools/codegen/pkg/modelgen/testdata"):
                if file.endswith(".descriptor_set"):
                    makelink(WKSPC + "/bazel-genfiles/tools/codegen/pkg/modelgen/testdata/" + file, WKSPC + "/tools/codegen/pkg/modelgen/testdata/" + file)

def config_proto(WKSPC, genfiles):
    if os.path.exists(genfiles + "com_github_istio_api/fixed_cfg.pb.go"):
        makelink(genfiles + "com_github_istio_api/fixed_cfg.pb.go", WKSPC + "/pkg/config/proto/fixed_cfg.pb.go")

def attributes_list(WKSPC, genfiles):
    if os.path.exists(WKSPC + "/bazel-genfiles/pkg/attribute/list.gen.go"):
        makelink(WKSPC + "/bazel-genfiles/pkg/attribute/list.gen.go", WKSPC + "/pkg/attribute/list.gen.go")

def inventory(WKSPC):
    if os.path.exists(WKSPC + "/bazel-genfiles/adapter/"):
        for file in os.listdir(WKSPC + "/bazel-genfiles/adapter/"):
            if file.endswith(".gen.go"):
                makelink(WKSPC + "/bazel-genfiles/adapter/" + file, WKSPC + "/adapter/" + file)

if __name__ == "__main__":
    import sys
    sys.exit(main(sys.argv[1:]))
