#!/usr/bin/env bash

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

