#!/usr/bin/env python

# utility functions to invoke bazel.

import subprocess

def bazel_info(name):
    return subprocess.check_output(["bazel", "info", name]).strip()

def bazel_query(q):
    p = subprocess.Popen(['bazel', 'query', q], stdout=subprocess.PIPE)
    (stdout, _) = p.communicate()
    return [l for l in stdout.split('\n') if l]

def bazel_build(targets):
    p = subprocess.Popen(['xargs', 'bazel', 'build'], stdin=subprocess.PIPE)
    (_, stderr) = p.communicate('\n'.join(targets))
    if p.returncode != 0:
        raise subprocess.CalledProcessError(
            returncode=p.returncode,
            cmd=['bazel', 'build'] + targets,
            output=stderr)
