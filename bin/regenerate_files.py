#!/usr/bin/env python

import json
import os
import shutil
import subprocess
import re

import bazel_util

def should_copy(src, dest):
    '''
    check if dest should be overwritten by src.

    Basically this means src is different from dest, but has some heuristics to ignore
    non significant changes for .pb.go files.
    '''
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

def node_to_path(node):
    '''
    get the filepath name from bazel node name (query result).

    The node name is in format of //path/to/directory:filename.
    '''
    return re.sub(r'//(.*):([^/]*)', '\\g<1>/\\g<2>', node)

def replace_extension(path, new_ext):
    '''replace the file extension of path to the new one.'''
    return os.path.splitext(path)[0] + new_ext

def get_generated_files(WKSPC, genfiles):
    '''get the files automatically generated for building istio.'''
    lst = []

    # bazel query for go_proto_library, like //pilot/test/grpcecho.
    proto_libraries = 'kind("go_proto_library", "//...")'

    # bazel query for proto_compiles. This is also used internally by gogoslick_proto_library
    # and mixer_proto_library. attr("langs", "go") filter is necessary, since some proto_compile
    # rules are for descriptors.
    proto_compiles = 'attr("langs", "go", kind("proto_compile", "//..."))'

    # bazel query for genrules. This is used for .gen.go files for mixer templates, but also
    # used by others like pilot/adapter/config/crd.
    genrules = 'attr("outs", ".go", kind("genrule", "//..."))'

    # First regenerate those codegens.
    bazel_util.bazel_build(bazel_util.bazel_query('+'.join([proto_libraries, proto_compiles, genrules])))

    # Second, get the list of generated files (like .pb.go files) from those build targets.
    # This can be done through labels() extraction and filtering of suffix of bazel query.
    for proto in bazel_util.bazel_query('filter("\.proto$", labels("srcs", %s))' % proto_libraries):
        pbgo = replace_extension(node_to_path(proto), '.pb.go')
        lst.append((os.path.join(genfiles, pbgo), os.path.join(WKSPC, pbgo)))
    for proto in bazel_util.bazel_query('filter("\.proto$", labels("protos", %s))' % proto_compiles):
        pbgo = replace_extension(node_to_path(proto), '.pb.go')
        src = os.path.join(genfiles, pbgo)
        lst.append((src, os.path.join(WKSPC, pbgo)))
    for genout in bazel_util.bazel_query('filter("\.go$", labels("outs", %s))' % genrules):
        genout = node_to_path(genout)
        lst.append((os.path.join(genfiles, genout), os.path.join(WKSPC, genout)))
    return lst

def generate_linters_conf(WKSPC, manifest):
    '''generates gometalinter config file which excludes generated files'''
    with open(os.path.join(WKSPC, "lintconfig_base.json")) as fin:
        conf = json.load(fin)
    conf['exclude'].extend(manifest)
    with open(os.path.join(WKSPC, "lintconfig.gen.json"), "wt") as fout:
        json.dump(conf, fout, sort_keys=True, indent=4, separators=(',', ': '))

def copy_autogen_files(WKSPC, genfiles, generated_files, manifest):
    '''copies auto-generated files from bazel-genfiles directory'''
    with open(WKSPC+"/generated_files", "wt") as fl:
      print >>fl, "#List of generated files that are checked in"
      for mm in manifest:
        print >>fl, mm

    for (src, dest) in generated_files:
        try:
            if should_copy(src, dest):
                shutil.copyfile(src, dest)
        except Exception as ex:
            print src, dest, ex

def regenerate(WKSPC, genfiles, regenerate_autogen_files=True):
    generated_files = get_generated_files(WKSPC, genfiles)
    bazel_util.bazel_build(['@io_istio_api//mixer/v1/config:config_fixed'])
    generated_files.append((genfiles + "/external/io_istio_api/mixer/v1/config/fixed_cfg.pb.go", WKSPC + "/mixer/pkg/config/proto/fixed_cfg.pb.go"))
    manifest = sorted([src[len(genfiles)+1:] for (src, _) in generated_files])

    generate_linters_conf(WKSPC, manifest)
    if regenerate_autogen_files:
        copy_autogen_files(WKSPC, genfiles, generated_files, manifest)


def main():
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument('--lintconfig_only', action='store_true')
    parser.add_argument('WKSPC', nargs='?', default=bazel_util.bazel_info('workspace'))
    args = parser.parse_args()
    regenerate(args.WKSPC, bazel_util.bazel_info("bazel-genfiles"), regenerate_autogen_files=not args.lintconfig_only)


if __name__ == "__main__":
    import sys
    sys.exit(main())
