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

file_list=(
		"../../../testdata/config/attributes.yaml"
		"../../../template/apikey/template.yaml"
		"../../../template/authorization/template.yaml"
		"../../../template/checknothing/template.yaml"
		"../../../template/listentry/template.yaml"
		"../../../template/logentry/template.yaml"
		"../../../template/metric/template.yaml"
		"../../../template/quota/template.yaml"
		"../../../template/reportnothing/template.yaml"
		"../../../template/tracespan/tracespan.yaml"
		"../../../test/spyAdapter/template/apa/tmpl.yaml"
		"../../../test/spyAdapter/template/checkoutput/tmpl.yaml"
)

go-bindata --nocompress --nometadata --pkg test -o fixtures.gen.go "${file_list[@]}"
