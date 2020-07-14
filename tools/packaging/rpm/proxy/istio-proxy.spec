# Those can be overridden when invoking make, eg: `make VERSION=2.0.0 rpm`
%global package_version 0.0.1
%global package_release 1

# https://github.com/istio/proxy
%global provider        github
%global provider_tld    com
%global project         istio
%global repo            proxy
%global provider_prefix %{provider}.%{provider_tld}/%{project}/%{repo}

# Use /usr/local as base dir, once upstream heavily depends on that
%global _prefix /usr/local
%global envoy_libdir /var/lib/istio/envoy

%global debug_package   %{nil}

Name:           istio-proxy
Version:        %{package_version}
Release:        %{package_release}%{?dist}
Summary:        The Istio Proxy is a microservice proxy that can be used on the client and server side, and forms a microservice mesh. The Proxy supports a large number of features.
License:        ASL 2.0
URL:            https://%{provider_prefix}

BuildRequires:  ninja-build
BuildRequires:  perl
BuildRequires:  binutils
BuildRequires:  cmake3

Source0:        istio-proxy.tar.gz
Source1:        sidecar.env
Source2:        envoy_bootstrap.json

%description
The Istio Proxy is a microservice proxy that can be used on the client and server side, and forms a microservice mesh. The Proxy supports a large number of features.

########### istio-proxy ###############
%package istio-proxy
Summary:  The istio envoy proxy

%description istio-proxy
The Istio Proxy is a microservice proxy that can be used on the client and server side, and forms a microservice mesh. The Proxy supports a large number of features.

This package contains the envoy program.

istio-proxy is the proxy required by the Istio Pilot Agent that talks to Istio pilot

%prep
%setup -q -n %{name}

%build
export CC=clang
export CXX=clang++
bazel --output_base=/builder/bazel_cache --output_user_root=/builder/bazel_cache/root  build --config=release //...
bazel shutdown

%install
rm -rf $RPM_BUILD_ROOT
install -d -m755 $RPM_BUILD_ROOT/%{_bindir}
install -d -m755 $RPM_BUILD_ROOT/%{envoy_libdir}

install -m755 ${RPM_BUILD_DIR}/istio-proxy/bazel-bin/src/envoy/envoy ${RPM_BUILD_ROOT}%{_bindir}
install -m644 %{SOURCE1} $RPM_BUILD_ROOT%{envoy_libdir}/sidecar.env
install -m644 %{SOURCE2} $RPM_BUILD_ROOT%{envoy_libdir}/envoy_bootstrap_tmpl.json

%files
%attr(0755,root,root) %{_bindir}/envoy
%attr(0644,root,root) %{envoy_libdir}/sidecar.env
%attr(0644,root,root) %{envoy_libdir}/envoy_bootstrap_tmpl.json

%changelog
* Tue Nov 19 2019 Idan Zach <zachidan@gmail.com>
- Upgrade envoy version
* Fri Feb 15 2019 Jonh Wendell <jonh.wendell@redhat.com>
- First release
