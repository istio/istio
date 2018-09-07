This file helps note down issues we have seen and how to debug them

1. If you have problem with the `make docker` step in the [Build istio images](README.md#3-build-istio-images) step in 
   [Readme](README.md), please try to clean up all the built binaries and run the test setup script again.

   ```bash
   cd $ISTIO/istio
   GOOS=linux make clean
   ```

   The `GOOS=linux` is required when you are running setup on macOS.

1. If Minikube is crashing or not setting up properly, ensure it's at version 0.27.0. That's a known version that works well with our setup.

   ```bash
   minikube version
   # Should return : minikube version: v0.27.0
   ```

1. When running tests, if you get "Bad Request" error
   Minikube uses insecure local registry opened at localhost:5000, make sure you specify HUB as `localhost:5000` and TAG as `latest` when running the tests.
1. If your machine complains of low disk space, try clean up docker images from it.
   To cleanup all docker images on your machine,run following command:

   ```bash
   docker images -q |xargs docker rmi
   ```

1. When installing docker if you see errors about missing packages on linux, please download and then retry docker installation.
1. If your prereqs installation, seems to be stuck, try restarting the box.