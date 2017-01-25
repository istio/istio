#!/bin/bash
set -e

# Generate SHA for the images and push it
TAG=$(git rev-parse HEAD)
HUB="gcr.io/istio-test"

# Creation step
create=true

while getopts :h:t:s arg; do
  case ${arg} in
    h) HUB="${OPTARG}";;
    t) TAG="${OPTARG}";;
    s) create=false;;
    *) echo "Invalid option: -${OPTARG}"; exit 1;;
  esac
done

# Get my templater
bazel build //test/integration
replacer=../../bazel-bin/test/integration/integration
out=echo.yaml

# Write template for k8s
$replacer manager.yaml.tmpl > $out <<eof
hub: $HUB
tag: $TAG
eof
$replacer http-service.yaml.tmpl >> $out <<eof
hub: $HUB
tag: $TAG
name: a
port1: "8080"
eof
$replacer http-service.yaml.tmpl >> $out <<eof
hub: $HUB
tag: $TAG
name: b
port1: "80"
eof


if [[ "$create" = true ]]; then
  gcloud docker --authorize-only
  for image in runtime app; do
    bazel run //docker:$image
    docker tag istio/docker:$image $HUB/$image:$TAG
    docker push $HUB/$image:$TAG
  done
  kubectl apply -f $out
fi

# Wait for pods to be ready
while : ; do
  kubectl get pods | grep -i "init\|creat\|error" || break
  sleep 1
done

# Get pod names
for pod in a b t; do
  declare "${pod}"="$(kubectl get pods -l app=$pod -o jsonpath='{range .items[*]}{@.metadata.name}')"
done

# Try all pairwise requests
tt=false
for src in a b t; do
  for dst in a b t; do
    url="http://${dst}/${src}"
    echo Requesting ${url} from ${src}...

    request=$(kubectl exec ${!src} -c app client ${url})

    echo $request | grep "X-Request-Id" ||\
      if [[ $src == "t" && $dst == "t" ]]; then
        tt=true
        echo Expected no request
      else
        echo Failed injecting proxy: request ${url}
        exit 1
      fi

    id=$(echo $request | grep -o "X-Request-Id=\S*" | cut -d'=' -f2-)
    echo x-request-id=$id

    # query access logs in src and dst
    for log in $src $dst; do
      if [[ $log != "t" ]]; then
        echo Checking access log of $log...

        n=1
        while : ; do
          if [[ $n == 30 ]]; then
            break
          fi
          kubectl logs ${!log} -c proxy | grep "$id" && break
          sleep 1
          ((n++))
        done

        if [[ $n == 30 ]]; then
          echo Failed to find request $id in access log of $log after $n attempts for $url
          exit 1
        fi
      fi
    done
  done
done

