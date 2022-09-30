# Testing TSB With A Custom Envoy Binary #

## Which envoy code base a particular proxyv2 image is running? ##

Proxyv2 image name has SHA of the commit in the tetrateio/istio repo. For example, in `tetrate/proxyv2:0.6.8-istio-f6cab05a8`, `f6cab05a8` is the SHA of the commit in tetrateio/istio
* Checkout to the commit SHA in tetrateio/istio
* [tetrateio/istio/istio.deps]((https://github.com/tetrateio/istio/blob/f6cab05a86011e59c59c0366e10f71019b35c13d/istio.deps#L7)) file has SHA of the commit in the istio/proxy repo.
* In [istio.io/proxy/WORKSPACE](https://github.com/istio/proxy/blob/e4df956fb629490dbc7af43ec9c5edda3245d45f/WORKSPACE#L41), there is SHA of the commit at the [envoyproxy/envoy-wasm](https://github.com/envoyproxy/envoy-wasm)
* envoyproxy/envoy-wasm is a fork of envoyproxy/envoy for wasm filter support development

## Letting GetEnvoy build Istio for you ##

If you don't want to go through setting up a VM for testing a particular envoy patch inside of TSB,
it is possible to allow the same infrastructure that builds our nightly getenvoy packages to build
a istio binary for you. To accomplish this you'll need a couple things:

1. A GCP account capable of running builds in the `getenvoy-package` project.
   (To check if you do run: `gcloud builds list --project getenvoy-package`)
   (If you don't have permission file a ticket for infosec).
1. A public envoy commit that has made it's way into `envoy-wasm`, or
   a `envoy-wasm` fork that is public.
   (If you have just merged a patch into `envoyproxy/envoy`, it will be
    mirrored over to `envoyproxy/envoy-wasm` soon. You could also just
    test your patch on your own fork of: `envoyproxy/envoy-wasm`.)
   (If you need to test a private patch such as a security release, please
    get in contact with the #eng-getenvoy channel in slack).

If you have these two things, congratulations you're ready to go.

1. Identify the Istio Proxy commit you'd like to use. More often than
   not this will be a SHA of `github.com/istio/proxy`, or a fork of it. If you
   just care about your patch to envoy, and do not care about a specific
   istio/proxy change, you can fetch the SHA `istio.deps` in this repository.
   It is the `PROXY_REPO_SHA` value.

   Save this value: `export ISTIO_PROXY_COMMIT="<my commit sha or tag>"`.
   (NOTE: if you're targeting a fork of `https://github.com/istio/proxy`,
    save the fork git url: `export ISTIO_PROXY_URL="<my git url>"`.)

1. Identify the Envoy Commit, and Envoy Git Repo you want to use. Remember it
   is required the envoy git url be based off of `envoyproxy/envoy-wasm`.
   You can either wait for `envoyproxy/envoy-wasm` to mirror over your change, or
   fork `envoyproxy/envoy-wasm` and pick your change (if you made it to
   `envoyproxy/envoy`).

   Save the two values:
   `export ENVOY_GIT_URL="<my git url like https://github.com/envoyproxy/envoy-wasm>"`,
   and `export ENVOY_SHA="<my commit sha or tag>"`.

1. Finally trigger a build of the envoy binary. To do this clone: `https://github.com/tetratelabs/getenvoy-ci`,
   and from the directory of `getenvoy-ci`, run:

   ```shell
   gcloud builds submit \
     --async \
     --no-source \
     --project getenvoy-package \
     --config cloudbuild/package_istio_proxy.yaml \
     --substitutions "_ENVOY_DIST=linux-glibc,_ENVOY_COMMIT=${ISTIO_PROXY_COMMIT},_ENVOY_REPO=${ISTIO_PROXY_URL:-"https://github.com/istio/proxy"},_OVERRIDE_ENVOY_REPO=${ENVOY_GIT_URL},_OVERRIDE_ENVOY_SHA=${ENVOY_SHA}"
   ```

   This will give you a URL to watch the build proceed (it should take around 20-30 minutes).
   At the end of the job it will give you a URL for bintray where you can download the binary
   for istio-proxy called "envoy" (and this will be shareable).

1. Finally move the envoy binary into: `out/linux_amd64/release/envoy`, and run: `make docker.proxyv2`.

## Building an Envoy Binary Manually ##

### Envoy dev env setup ###

Prepare ubuntu vm preferabbly on gcp as explained [here](https://github.com/tetratelabs/getenvoy-package/wiki/Envoy-dev-env-setup)

### Building proxyv2 image with custom envoy binary ###

* On the ubuntu vm, `git clone https://github.com/istio/proxy` and `git clone https://github.com/envoyproxy/envoy-wasm`
* Let say proxy and envoy-wasm are checked out at /home/vikas/
* For remote build execution, add following in /home/vikas/proxy/.bazelrc:

  ```shell
  build --remote_instance_name=projects/getenvoy-package/instances/default_instance
  build --config=remote-clang-libc++
  build --config=remote-ci
  build --jobs=80
  build --remote_download_outputs=all
  ```

* Make your changes in the /home/vikas/envoy-wasm

  ```shell
  export BAZEL_BUILD_ARGS="--override_repository=envoy=/home/vikas/envoy-wasm"
  cd /home/vikas/proxy; make
  ```

* On local machine

  ```shell
  cd <istio-repo-path>
  make init
  scp -i <gcp-key> <ubuntu-vm-ip>:/home/vikas/proxy/bazel-bin/src/envoy/envoy out/linux_amd64/release/envoy
  make docker.proxyv2
  ```
