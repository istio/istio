#remove leading characters since package version expects to start with digit
PACKAGE_VERSION ?= $(shell echo $(VERSION) | sed 's/^[a-z]*-//' | sed 's/-//')

# Building the debian file, docker.istio.deb and istio.deb
include tools/packaging/deb/istio.mk

# RPM/RHEL/CENTOS stuff
include tools/packaging/rpm/rpm.mk
