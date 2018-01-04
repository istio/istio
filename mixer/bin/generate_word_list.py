#!/usr/bin/env python

#
# Generates golang from a yaml-formatted global attributes list.
#

import os
import argparse

HEADER = """// Copyright 2017 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package attribute

func GlobalList() ([]string) { 
    tmp := make([]string, len(globalList))
    copy(tmp, globalList)
    return tmp
}

var ( 
    globalList = []string{
"""

FOOTER = """    }
)
"""

def generate(src, dst):
    code = HEADER
    for line in src:
        if line.startswith("-"):
            code += "\t\t\"" + line[1:].strip() + "\",\n"
    code += FOOTER
    dst.write(code)

def main(args):
    parser = argparse.ArgumentParser(description='Generate global world list code.')
    parser.add_argument('infile', type=argparse.FileType('r'), help='source file for global word list')
    parser.add_argument('outfile', type=argparse.FileType('w'), help='output file for generated code')
    parsed = parser.parse_args(args)
    generate(parsed.infile, parsed.outfile)
    parsed.infile.close()
    parsed.outfile.close()


if __name__ == "__main__":
    import sys
    sys.exit(main(sys.argv[1:]))
