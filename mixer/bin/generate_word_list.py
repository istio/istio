#!/usr/bin/env python

#
# Generates golang from a yaml-formatted global attributes list.
#

import os

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

FOOTER = """}
)
"""

def generate(src_path):
    code = HEADER
    with open(src_path) as src_file:
        for line in src_file:
            if line.startswith("-"):
                code += "\"" + line[1:].strip() + "\",\n"
    code += FOOTER
    print(code)

def main(args):
    if len(args) == 1:
        file = args[0]
        path = os.path.abspath(file)
        generate(path)
    else:
        print "USAGE: generate_word_list.py <path_to_yaml_src>"

if __name__ == "__main__":
    import sys
    sys.exit(main(sys.argv[1:]))
