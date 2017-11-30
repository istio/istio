#!/usr/bin/env python

import json
import os
import shutil
import subprocess
import re

import bazel_util

def should_copy(src, dest):
    if not os.path.exists(dest):
        return True
    p = subprocess.Popen(['diff', '-u', src, dest], stdout=subprocess.PIPE)
    (stdout, _) = p.communicate()
    if src.endswith('.pb.go'):
        # there might be diffs for 'source:' lines and others.
        has_diff = False
        linecount = 0
        for l in stdout.split('\n'):
            linecount += 1
            if not l:
                continue
            if linecount < 3:
                # first two lines are headers, skipping
                continue
            if l.startswith('@@ '):
                # header for a diff chunk
                continue
            if l.startswith(' '):
                # part of non-changes
                continue
            if re.search(r'genfiles/.*\.proto\b', l):
                # ignoring the proto file path -- the exact file path can be different,
                # depending on the platform.
                continue
            if re.search(r'// \d+ bytes of a gzipped FileDescriptorProto', l):
                # header comment for descriptor bytes data. It can be different if the file path
                # is different.
                continue
            if re.match(r'^.\s*(0x[0-9a-f][0-9a-f],\s*)+$', l):
                # file descriptor bytes data. It can be different if the file path is different.
                continue
            # Other changes would be meaningful changes.
            has_diff = True
            break
    else:
        has_diff = (stdout != '')
    return has_diff

def get_generated_files(WKSPC, genfiles):
    lst = []
    proto_libraries = 'kind("go_proto_library", "//...")'
    proto_compiles = 'attr("langs", "go", kind("proto_compile", "//..."))'
    genrules = 'attr("outs", ".go", kind("genrule", "//..."))'
    bazel_util.bazel_build(bazel_util.bazel_query('+'.join([proto_libraries, proto_compiles, genrules])))

    for proto in bazel_util.bazel_query('filter("\.proto$", labels("srcs", %s))' % proto_libraries):
        proto = re.sub(r'//(.*):([^/]*)', '\\g<1>/\\g<2>', proto)
        pbgo = proto[:len(proto)-len('.proto')] + '.pb.go'
        lst.append((os.path.join(genfiles, pbgo), os.path.join(WKSPC, pbgo)))
    for proto in bazel_util.bazel_query('filter("\.proto$", labels("protos", %s))' % proto_compiles):
        proto = re.sub(r'//(.*):([^/]*)', '\\g<1>/\\g<2>', proto)
        pbgo = proto[:len(proto)-len('.proto')] + '.pb.go'
        src = os.path.join(genfiles, pbgo)
        lst.append((src, os.path.join(WKSPC, pbgo)))
    for genout in bazel_util.bazel_query('filter("\.go$", labels("outs", %s))' % genrules):
        genout = re.sub(r'//(.*):([^/]*)', '\\g<1>/\\g<2>', genout)
        lst.append((os.path.join(genfiles, genout), os.path.join(WKSPC, genout)))
    return lst

def regenerate(WKSPC, genfiles):
    generated_files = get_generated_files(WKSPC, genfiles)
    bazel_util.bazel_build(['@io_istio_api//mixer/v1/config:config_fixed'])
    generated_files.append((genfiles + "/external/io_istio_api/mixer/v1/config/fixed_cfg.pb.go", WKSPC + "/mixer/pkg/config/proto/fixed_cfg.pb.go"))

    # generate manifest of generated files
    manifest = sorted([src[len(genfiles)+1:] for (src, _) in generated_files])
    with open(WKSPC+"/generated_files", "wt") as fl:
      print >>fl, "#List of generated files that are checked in"
      for mm in manifest:
        print >>fl, mm

    # generate gometalinter config file which excludes generated files
    with open(os.path.join(WKSPC, "lintconfig_base.json")) as fin:
        conf = json.load(fin)
    conf['exclude'].extend(manifest)
    with open(os.path.join(WKSPC, "lintconfig.gen.json"), "wt") as fout:
        json.dump(conf, fout, sort_keys=True, indent=4, separators=(',', ': '))

    for (src, dest) in generated_files:
        try:
            if should_copy(src, dest):
                shutil.copyfile(src, dest)
        except Exception as ex:
            print src, dest, ex

def main(args):
    WKSPC = bazel_util.bazel_info('workspace')
    if len(args) > 0:
        WKSPC = args[0]
    regenerate(WKSPC, bazel_util.bazel_info("bazel-genfiles"))


if __name__ == "__main__":
    import sys
    sys.exit(main(sys.argv[1:]))
