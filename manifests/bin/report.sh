#!/usr/bin/env bash

# Copyright Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Generate junit reports

JUNIT_REPORT=${JUNIT_REPORT:-/usr/local/bin/go-junit-report}

# Running in machine - nothing pre-installed
if [ -nf ${JUNIT_REPORT} ] ; then
    make ${GOPATH}/bin/go-junit-report
    JUNIT_REPORT=${GOPATH}/bin/go-junit-report
fi


for $i in ${GOPATH}/out/logs/*.log ; do \
	cat $i | $(JUNIT_REPORT) > ${GOPATH}/out/report/$(i).xml
done

