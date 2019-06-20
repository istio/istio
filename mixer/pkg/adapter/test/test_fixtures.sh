#!/usr/bin/env bash

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
