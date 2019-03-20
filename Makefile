BASE := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))

EXTRA ?= --set global.hub=${HUB} --set global.tag=${TAG}
OUT ?= ${BASE}/out


control:
	${BASE}/bin/iop istio-system citadel ${BASE}/security/citadel ${ARGS}
	# TODO: move the other components from env.sh

cleanup:
	# TODO: remove webhooks and global resources


localci:





