# Bookinfo Sample

See <https://istio.io/docs/examples/bookinfo/>.

**Note**: We need the owner of the PR to perform the appropriate testing with built/pushed images to their own docker repository before we would build/push images to the official Istio repository.

## General Setup

```bash
# This defines the docker hub to use when running integration tests and building docker images
# eg: HUB="docker.io/istio", HUB="gcr.io/istio-testing"
export HUB="docker.io/$USER"

# This defines the docker tag to use when running integration tests and
# building docker images to be your user id. You may also set this variable
# this to any other legitimate docker tag.
export TAG=<version number>
```

## Compile code

```bash
cd samples/bookinfo
BOOKINFO_TAG=$TAG BOOKINFO_HUB=$HUB src/build-services.sh
```

For example:

```bash
$ BOOKINFO_TAG=test1.0 BOOKINFO_HUB=docker.io/user1  src/build-services.sh
+++ dirname ./build-services.sh
++ cd .
++ pwd
+ SCRIPTDIR=/work/samples/bookinfo/src
+ cd /work/samples/bookinfo/src/../../..
+ h=docker.io/user1
+ t=test1.0
+ [[ docker.io/user1 == \i\s\t\i\o ]]
+ [[ docker.io/user1 == \d\o\c\k\e\r\.\i\o\/\i\s\t\i\o ]]
+ plat=linux/amd64
+ [[ '' == \t\r\u\e ]]
+ env TAG=test1.0 HUB=docker.io/user1 docker buildx bake -f samples/bookinfo/src/docker-bake.hcl --set '*.platform=linux/amd64'
[+] Building 1.9s (123/133)
 => [examples-bookinfo-ratings-v-faulty internal] load build definition from Dockerfile                                                                                                               0.0s
 => => transferring dockerfile: 1.05kB                                                                                                                                                                0.0s
...
 => CACHED [examples-bookinfo-ratings-v-faulty 4/6] COPY ratings.js /opt/microservices/                                                                                                               0.0s
 => CACHED [examples-bookinfo-ratings-v-faulty 5/6] WORKDIR /opt/microservices                                                                                                                        0.0s
 => CACHED [examples-bookinfo-ratings-v-faulty 6/6] RUN npm install                                                                                                                                   0.0s
WARNING: No output specified for examples-bookinfo-mysqldb, examples-bookinfo-ratings-v-faulty, examples-bookinfo-reviews-v2, examples-bookinfo-reviews-v3, examples-bookinfo-productpage-v-flooding, examples-bookinfo-ratings-v-unhealthy, examples-bookinfo-ratings-v-unavailable, examples-bookinfo-ratings-v1, examples-bookinfo-details-v2, examples-bookinfo-reviews-v1, examples-bookinfo-productpage-v1, examples-bookinfo-ratings-v-delayed, examples-bookinfo-details-v1, examples-bookinfo-ratings-v2, examples-bookinfo-mongodb target(s) with docker-container driver. Build result will only remain in the build cache. To push result image into registry use --push or to load image into docker use --load
```

The code for the bookinfo sample is now compiled and built.  The bookinfo versions are different from Istio versions since the sample should work with any version of Istio.

## Build docker images

```bash
cd samples/bookinfo
BOOKINFO_TAG=$TAG BOOKINFO_HUB=$HUB src/build-services.sh --load
```

For example:

```bash
$ BOOKINFO_TAG=test1.0 BOOKINFO_HUB=docker.io/user1  src/build-services.sh --load
+++ dirname ./build-services.sh
++ cd .
++ pwd
+ SCRIPTDIR=/work/samples/bookinfo/src
+ cd /work/samples/bookinfo/src/../../..
+ h=docker.io/user1
+ t=test1.0
+ [[ docker.io/user1 == \i\s\t\i\o ]]
+ [[ docker.io/user1 == \d\o\c\k\e\r\.\i\o\/\i\s\t\i\o ]]
+ plat=linux/amd64
+ [[ '' == \t\r\u\e ]]
...
 => [examples-bookinfo-productpage-v-flooding] exporting to docker image format                                                                                                                      10.4s
 => => exporting layers                                                                                                                                                                               0.0s
 => => exporting manifest sha256:5046deeca78c67f0977fa627b3c2a98ba380b09f4dabf5620040fbf723785f6a                                                                                                     0.0s
 => => exporting config sha256:5a632c874e649f6492d5a6592a3da2b9ee3fca8d6f55bfbc0249b865eb8579be                                                                                                       0.0s
 => => sending tarball                                                                                                                                                                               10.4s
 => importing to docker                                                                                                                                                                               0.1s
 => importing to docker                                                                                                                                                                               0.0s
 => importing to docker                                                                                                                                                                               0.0s
 => importing to docker                                                                                                                                                                               0.0s
 => importing to docker                                                                                                                                                                               0.0s
 => importing to docker                                                                                                                                                                               0.0s
 => importing to docker                                                                                                                                                                               0.0s
 => importing to docker                                                                                                                                                                               0.0s
 => importing to docker                                                                                                                                                                               0.0s
 => importing to docker                                                                                                                                                                               0.0s
 => importing to docker                                                                                                                                                                               0.0s
 => importing to docker                                                                                                                                                                               0.1s
 => importing to docker                                                                                                                                                                               0.3s
 => importing to docker                                                                                                                                                                               0.2s
 => importing to docker                                                                                                                                                                               0.1s
+ [[ true == \t\r\u\e ]]
+ find ./samples/bookinfo/platform -name '*bookinfo*.yaml' -exec sed -i.bak 's#image:.*\(\/examples-bookinfo-.*\):.*#image: docker.io\/user1\1:test1.0#g' '{}' +/ay
```

Docker images are now created.

## Push docker images to docker hub

After the local build is successful, you will need to push the images to Docker hub.  You may need to login to Docker before you run the command using `docker login`.

```bash
cd samples/bookinfo
BOOKINFO_LATEST=true BOOKINFO_TAG=$TAG BOOKINFO_HUB=$HUB src/build-services.sh --push
```

For example:

```bash
$ BOOKINFO_TAG=test1.0 BOOKINFO_HUB=docker.io/user1  src/build-services.sh --push
+++ dirname ./build-services.sh
++ cd .
++ pwd
+ SCRIPTDIR=/work/samples/bookinfo/src
+ cd /work/samples/bookinfo/src/../../..
+ h=docker.io/user1
+ t=test1.0
+ [[ docker.io/user1 == \i\s\t\i\o ]]
+ [[ docker.io/user1 == \d\o\c\k\e\r\.\i\o\/\i\s\t\i\o ]]
+ plat=linux/amd64
+ [[ '' == \t\r\u\e ]]
+ env TAG=test1.0 HUB=docker.io/user1 docker buildx bake -f samples/bookinfo/src/docker-bake.hcl --set '*.platform=linux/amd64' --push
...
 => => pushing layers                                                                                                                                                                                11.1s
 => => pushing manifest for docker.io/user1/examples-bookinfo-reviews-v3:test1.0@sha256:4c9e2dfcabdfc55fba9037967ee412690b23d676481713eb88985926e229c8db                                          0.7s
 => [auth] user1/examples-bookinfo-ratings-v2:pull,push token for registry-1.docker.io                                                                                                            0.0s
 => [auth] user1/examples-bookinfo-ratings-v-delayed:pull,push token for registry-1.docker.io                                                                                                     0.0s
 => [auth] user1/examples-bookinfo-ratings-v-unavailable:pull,push token for registry-1.docker.io                                                                                                 0.0s
 => [auth] user1/examples-bookinfo-ratings-v-unhealthy:pull,push token for registry-1.docker.io                                                                                                   0.0s
 => [auth] user1/examples-bookinfo-ratings-v-faulty:pull,push token for registry-1.docker.io                                                                                                      0.0s
 => [auth] user1/examples-bookinfo-mongodb:pull,push token for registry-1.docker.io                                                                                                               0.0s
 => [auth] user1/examples-bookinfo-details-v1:pull,push token for registry-1.docker.io                                                                                                            0.0s
 => [auth] user1/examples-bookinfo-productpage-v1:pull,push token for registry-1.docker.io                                                                                                        0.0s
 => [auth] user1/examples-bookinfo-details-v2:pull,push token for registry-1.docker.io                                                                                                            0.0s
 => [auth] user1/examples-bookinfo-productpage-v-flooding:pull,push token for registry-1.docker.io                                                                                                0.0s
 => [auth] user1/examples-bookinfo-reviews-v1:pull,push token for registry-1.docker.io                                                                                                            0.0s
 => [auth] user1/examples-bookinfo-reviews-v3:pull,push token for registry-1.docker.io                                                                                                            0.0s
 => [auth] user1/examples-bookinfo-reviews-v2:pull,push token for registry-1.docker.io                                                                                                            0.0s
+ [[ true == \t\r\u\e ]]
+ find ./samples/bookinfo/platform -name '*bookinfo*.yaml' -exec sed -i.bak 's#image:.*\(\/examples-bookinfo-.*\):.*#image: docker.io\/user1\1:test1.0#g' '{}' +
```

## Update YAML files to point to the newly created images

You need to update the YAML file with the latest tag that you used during the build, eg: `$HUB:$TAG`.

Run the following script to update the YAML files in one step.

```bash
cd samples/bookinfo
export BOOKINFO_UPDATE=true
BOOKINFO_TAG=test1.0 BOOKINFO_HUB=user1 src/build-services.sh
```

For example:

```bash
$ export BOOKINFO_UPDATE=true
$ BOOKINFO_TAG=test1.0 BOOKINFO_HUB=user1 src/build-services.sh
+++ dirname samples/bookinfo/src/build-services.sh
++ cd samples/bookinfo/src
++ pwd
+ SCRIPTDIR=/work/samples/bookinfo/src
+ cd /work/samples/bookinfo/src/../../..
+ h=user1
+ t=test1.0
+ [[ user1 == \i\s\t\i\o ]]
+ [[ user1 == \d\o\c\k\e\r\.\i\o\/\i\s\t\i\o ]]
+ plat=linux/amd64
+ [[ '' == \t\r\u\e ]]
+ env TAG=test1.0 HUB=docker.io/user1 docker buildx bake -f samples/bookinfo/src/docker-bake.hcl --set '*.platform=linux/amd64'
...
 => CACHED [examples-bookinfo-ratings-v-faulty 4/6] COPY ratings.js /opt/microservices/                                                                                                               0.0s
 => CACHED [examples-bookinfo-ratings-v-faulty 5/6] WORKDIR /opt/microservices                                                                                                                        0.0s
 => CACHED [examples-bookinfo-ratings-v-faulty 6/6] RUN npm install                                                                                                                                   0.0s
WARNING: No output specified for examples-bookinfo-mysqldb, examples-bookinfo-ratings-v-faulty, examples-bookinfo-reviews-v2, examples-bookinfo-reviews-v3, examples-bookinfo-productpage-v-flooding, examples-bookinfo-ratings-v-unhealthy, examples-bookinfo-ratings-v-unavailable, examples-bookinfo-ratings-v1, examples-bookinfo-details-v2, examples-bookinfo-reviews-v1, examples-bookinfo-productpage-v1, examples-bookinfo-ratings-v-delayed, examples-bookinfo-details-v1, examples-bookinfo-ratings-v2, examples-bookinfo-mongodb target(s) with docker-container driver. Build result will only remain in the build cache. To push result image into registry use --push or to load image into docker use --load
+ [[ true == \t\r\u\e ]]
+ find ./samples/bookinfo/platform -name '*bookinfo*.yaml' -exec sed -i.bak 's#image:.*\(\/examples-bookinfo-.*\):.*#image: user1\1:test1.0#g' '{}' +
```

Verify that expected image eg: `user1/examples-bookinfo-*:test1.0` is updated in `platform/kube/bookinfo*.yaml` files.

## Tests

Test that the bookinfo samples work with the latest image eg: `user1/examples-bookinfo-*:test1.0` that you pushed.

```bash
$ cd ../../
$ kubectl apply -f samples/bookinfo/platform/kube/bookinfo.yaml
serviceaccount/bookinfo-details created
deployment.apps/details-v1 created
serviceaccount/bookinfo-ratings created
...
```

Wait for all the pods to be in `Running` start.

```bash
$ kubectl get pods
NAME                              READY   STATUS    RESTARTS   AGE
details-v1-7f556f5c6b-485l2       2/2     Running   0          10m
productpage-v1-84c8f95c8d-tlml2   2/2     Running   0          10m
ratings-v1-66777f856b-2ls78       2/2     Running   0          10m
reviews-v1-64c47f4f44-rx642       2/2     Running   0          10m
reviews-v2-66b6b95f44-s5nt6       2/2     Running   0          10m
reviews-v3-7f69dd7fd4-zjvc8       2/2     Running   0          10m
```

Once all the pods are in the `Running` state. Test if the bookinfo works through cli.

```bash
$ kubectl exec -it "$(kubectl get pod -l app=ratings -o jsonpath='{.items[0].metadata.name}')" -c ratings -- curl productpage:9080/productpage | grep -o "<title>.*</title>"
<title>Simple Bookstore App</title>
```

You can also test it by hitting productpage in the browser.

```bash
http://192.168.39.116:31395/productpage
```

You should see the following in the browser.

![star](https://user-images.githubusercontent.com/2920003/86032538-212ff900-ba55-11ea-9492-d4bc90656a02.png)

**Note**: If everything works as mentioned above, request a new official set of images be built and pushed from the reviewer, and add another commit to the original PR with the version changes.

Bookinfo is tested by istio.io integration tests. You can find them under [tests](https://github.com/istio/istio.io/tree/master/tests) in the [istio/istio.io](https://github.com/istio/istio.io) repository.
