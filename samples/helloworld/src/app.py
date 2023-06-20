#!/usr/bin/python
#
# Copyright Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

import os
import math
from flask import Flask, request
app = Flask(__name__)


@app.route('/hello')
def hello():
    version = os.environ.get('SERVICE_VERSION')

    # do some cpu intensive computation
    x = 0.0001
    for i in range(0, 1000000):
        x = x + math.sqrt(x)

    return 'Hello version: %s, instance: %s\n' % (version, os.environ.get('HOSTNAME'))


@app.route('/health')
def health():
    return 'Helloworld is healthy', 200


if __name__ == "__main__":
    app.run(host='::', threaded=True)
