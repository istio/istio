# Build with debug info rpm
%global with_debug 0
# Run unit tests
%global with_tests 0
# Build test binaries
%global with_test_binaries 0

%if 0%{?with_debug}
%global _dwz_low_mem_die_limit 0
%else
%global debug_package   %{nil}
%endif

# Those can be overridden when invoking make, eg: `make VERSION=2.0.0 rpm`
%global package_version 0.0.1
%global package_release 1

%global provider        github
%global provider_tld    com
%global project         istio
%global repo            istio
# https://github.com/istio/istio
%global provider_prefix %{provider}.%{provider_tld}/%{project}/%{repo}
%global import_path     istio.io/istio

# Use /usr/local as base dir, once upstream heavily depends on that
%global _prefix /usr/local

Name:           istio
Version:        %{package_version}
Release:        %{package_release}%{?dist}
Summary:        An open platform to connect, manage, and secure microservices
License:        ASL 2.0
URL:            https://%{provider_prefix}

Source0:        istio.tar.gz
#Source1:        istiorc
#Source2:        buildinfo
Source3:        istio-start.sh
Source4:        istio-node-agent-start.sh
Source5:        istio-iptables.sh
Source6:        istio.service
Source7:        istio-auth-node-agent.service

# e.g. el6 has ppc64 arch without gcc-go, so EA tag is required
ExclusiveArch:  %{?go_arches:%{go_arches}}%{!?go_arches:%{ix86} x86_64 aarch64 %{arm}}
# If go_compiler is not set to 1, there is no virtual provide. Use golang instead.
BuildRequires:  golang >= 1.12

%description
Istio is an open platform that provides a uniform way to connect, manage
and secure microservices. Istio supports managing traffic flows between
microservices, enforcing access policies, and aggregating telemetry data,
all without requiring changes to the microservice code.

########### pilot-discovery ###############
%package pilot-discovery
Summary:  The istio pilot discovery
Requires: istio = %{version}-%{release}

%description pilot-discovery
Istio is an open platform that provides a uniform way to connect, manage
and secure microservices. Istio supports managing traffic flows between
microservices, enforcing access policies, and aggregating telemetry data,
all without requiring changes to the microservice code.

This package contains the pilot-discovery program.

pilot-discovery is the main pilot component and belongs to Control Plane.

########### pilot-agent ###############
%package pilot-agent
Summary:  The istio pilot agent
Requires: istio = %{version}-%{release}

%description pilot-agent
Istio is an open platform that provides a uniform way to connect, manage
and secure microservices. Istio supports managing traffic flows between
microservices, enforcing access policies, and aggregating telemetry data,
all without requiring changes to the microservice code.

This package contains the pilot-agent program.

pilot-agent is agent that talks to Istio pilot. It belongs to Data Plane.
Along with Envoy, makes up the proxy that goes in the sidecar along with applications.

########### istioctl ###############
%package istioctl
Summary:  The istio command line tool
Requires: istio = %{version}-%{release}

%description istioctl
Istio is an open platform that provides a uniform way to connect, manage
and secure microservices. Istio supports managing traffic flows between
microservices, enforcing access policies, and aggregating telemetry data,
all without requiring changes to the microservice code.

This package contains the istioctl program.

istioctl is the configuration command line utility.

########### sidecar-injector ###############
%package sidecar-injector
Summary:  The istio sidecar injector
Requires: istio = %{version}-%{release}

%description sidecar-injector
Istio is an open platform that provides a uniform way to connect, manage
and secure microservices. Istio supports managing traffic flows between
microservices, enforcing access policies, and aggregating telemetry data,
all without requiring changes to the microservice code.

This package contains the sidecar-injector program.

sidecar-injector is the Kubernetes injector for Istio sidecar.
It belongs to Control Plane.

########### mixs ###############
%package mixs
Summary:  The istio mixs
Requires: istio = %{version}-%{release}

%description mixs
Istio is an open platform that provides a uniform way to connect, manage
and secure microservices. Istio supports managing traffic flows between
microservices, enforcing access policies, and aggregating telemetry data,
all without requiring changes to the microservice code.

This package contains the mixs program.

mixs is the main mixer (server) component. Belongs to Control Plane.

########### mixc ###############
%package mixc
Summary:  The istio mixc
Requires: istio = %{version}-%{release}

%description mixc
Istio is an open platform that provides a uniform way to connect, manage
and secure microservices. Istio supports managing traffic flows between
microservices, enforcing access policies, and aggregating telemetry data,
all without requiring changes to the microservice code.

This package contains the mixc program.

mixc is a debug/development CLI tool to interact with Mixer API.

########### citadel ###############
%package citadel
Summary:  Istio Security Component
Requires: istio = %{version}-%{release}

%description citadel
Istio is an open platform that provides a uniform way to connect, manage
and secure microservices. Istio supports managing traffic flows between
microservices, enforcing access policies, and aggregating telemetry data,
all without requiring changes to the microservice code.

This package contains the istio_ca program.

This is the Istio Certificate Authority (CA) + security components.

########### galley ###############
%package galley
Summary:  Istio Galley Component
Requires: istio = %{version}-%{release}

%description galley
Istio is an open platform that provides a uniform way to connect, manage
and secure microservices. Istio supports managing traffic flows between
microservices, enforcing access policies, and aggregating telemetry data,
all without requiring changes to the microservice code.

This package contains the galley program.

Galley is responsible for configuration management in Istio.

########### node-agent ###############
%package node-agent
Summary:  The Istio Node Agent
Requires: istio = %{version}-%{release}

%description node-agent
Istio is an open platform that provides a uniform way to connect, manage
and secure microservices. Istio supports managing traffic flows between
microservices, enforcing access policies, and aggregating telemetry data,
all without requiring changes to the microservice code.

This package contains the node agent.


%if 0%{?with_test_binaries}

########### tests ###############
%package pilot-tests
Summary:  Istio Pilot Test Binaries
Requires: istio = %{version}-%{release}

%description pilot-tests
Istio is an open platform that provides a uniform way to connect, manage
and secure microservices. Istio supports managing traffic flows between
microservices, enforcing access policies, and aggregating telemetry data,
all without requiring changes to the microservice code.

This package contains the binaries needed for pilot tests.

%endif

%prep

rm -rf ISTIO
mkdir -p ISTIO/src/istio.io/istio
tar zxf %{SOURCE0} -C ISTIO/src/istio.io/istio --strip=1

#cp %{SOURCE1} ISTIO/src/istio.io/istio/.istiorc.mk
#cp %{SOURCE2} ISTIO/src/istio.io/istio/buildinfo

%build
cd ISTIO
export GOPATH=$(pwd):%{gopath}

pushd src/istio.io/istio
make pilot-discovery pilot-agent istioctl sidecar-injector mixc mixs citadel galley node_agent

%if 0%{?with_test_binaries}
make test-bins
%endif

popd

%install
rm -rf $RPM_BUILD_ROOT
install -d -m755 $RPM_BUILD_ROOT/%{_bindir}
install -d -m755 $RPM_BUILD_ROOT/%{_unitdir}

install -m755 %{SOURCE3} $RPM_BUILD_ROOT/%{_bindir}/istio-start.sh
install -m755 %{SOURCE4} $RPM_BUILD_ROOT/%{_bindir}/istio-node-agent-start.sh
install -m755 %{SOURCE5} $RPM_BUILD_ROOT/%{_bindir}/istio-iptables.sh

install -m644 %{SOURCE6} $RPM_BUILD_ROOT/%{_unitdir}/istio.service
install -m644 %{SOURCE7} $RPM_BUILD_ROOT/%{_unitdir}/istio-auth-node-agent.service

binaries=(pilot-discovery pilot-agent istioctl sidecar-injector mixs mixc istio_ca galley node_agent)
pushd .
cd ISTIO/out/linux_amd64/release
%if 0%{?with_debug}
    for i in "${binaries[@]}"; do
        cp -pav $i $RPM_BUILD_ROOT%{_bindir}/
%else
    mkdir stripped
    for i in "${binaries[@]}"; do
        echo stripping: $i
        strip -o stripped/$i -s $i
        cp -pav stripped/$i $RPM_BUILD_ROOT%{_bindir}/
    done
%endif
popd

%if 0%{?with_test_binaries}
cp -pav ISTIO/out/linux_amd64/release/{pilot-test-server,pilot-test-client,pilot-test-eurekamirror} $RPM_BUILD_ROOT%{_bindir}/
%endif

%if 0%{?with_tests}

%check
cd ISTIO
export GOPATH=$(pwd):%{gopath}
pushd src/istio.io/istio
make localTestEnv test
make localTestEnvCleanup
popd

%endif

%pre pilot-agent
getent group istio-proxy >/dev/null || groupadd --system istio-proxy || :
getent passwd istio-proxy >/dev/null || \
  useradd -c "Istio Proxy User" --system -g istio-proxy \
  -s /sbin/nologin -d /var/lib/istio istio-proxy 2> /dev/null || :

mkdir -p /var/lib/istio/{envoy,proxy,config} /var/log/istio /etc/certs
touch /var/lib/istio/config/mesh
chown -R istio-proxy.istio-proxy /var/lib/istio/ /var/log/istio /etc/certs

ln -s -T /var/lib/istio /etc/istio 2> /dev/null || :

%post pilot-agent
%systemd_post istio.service

%preun pilot-agent
%systemd_preun istio.service

%postun pilot-agent
%systemd_postun_with_restart istio.service

%pre node-agent
getent group istio-proxy >/dev/null || groupadd --system istio-proxy || :
getent passwd istio-proxy >/dev/null || \
  useradd -c "Istio Proxy User" --system -g istio-proxy \
  -s /sbin/nologin -d /var/lib/istio istio-proxy 2> /dev/null || :

mkdir -p /var/lib/istio/{envoy,proxy,config} /var/log/istio /etc/certs
touch /var/lib/istio/config/mesh
chown -R istio-proxy.istio-proxy /var/lib/istio/ /var/log/istio /etc/certs

ln -s -T /var/lib/istio /etc/istio 2> /dev/null || :

#define license tag if not already defined
%{!?_licensedir:%global license %doc}

%files
%license ISTIO/src/istio.io/istio/LICENSE
%doc     ISTIO/src/istio.io/istio/README.md

%files pilot-discovery
%{_bindir}/pilot-discovery

%files pilot-agent
%attr(2755,root,root) %{_bindir}/pilot-agent
%attr(0755,root,root) %{_bindir}/istio-start.sh
%attr(0755,root,root) %{_bindir}/istio-iptables.sh
%attr(0644,root,root) %{_unitdir}/istio.service

%files istioctl
%{_bindir}/istioctl

%files sidecar-injector
%{_bindir}/sidecar-injector

%files mixs
%{_bindir}/mixs

%files mixc
%{_bindir}/mixc

%files citadel
%{_bindir}/istio_ca

%files galley
%{_bindir}/galley

%files node-agent
%attr(0755,root,root) %{_bindir}/node_agent
%attr(0755,root,root) %{_bindir}/istio-node-agent-start.sh
%attr(0644,root,root) %{_unitdir}/istio-auth-node-agent.service

%if 0%{?with_test_binaries}
%files pilot-tests
%{_bindir}/pilot-test-server
%{_bindir}/pilot-test-client
%{_bindir}/pilot-test-eurekamirror
%endif

%changelog
* Thu Feb 7 2019 Jonh Wendell <jonh.wendell@redhat.com> - 1.1.0-1
- First package
