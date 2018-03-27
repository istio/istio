# Deploying Mixer With Custom Adapters

Congratulations! You've managed to build a complete Mixer adapter and have validated
that it works via local testing.

Follow these steps to deploy a Mixer with your custom adapter to your service
mesh for use:
  * [Build a docker image of Mixer with your custom adapter](#make-and-push-a-docker-image-of-mixer-with-your-custom-adapter)
  * [Generate the appropriate CRD for your custom adapter](#generate-the-appropriate-crd-for-your-custom-adapter)
  * [Deploy your CRD](#deploy-your-crd)
  * [Edit the Mixer deployment configuration to reference your new image](#edit-the-mixer-deployment-configuration-to-reference-your-new-image)
  * [Celebrate](#celebrate)

NOTE: 
  * All commands should be executed from the root of the istio.io/istio directory.
  * These instructions assume that the steps will be executed on a Linux distro. 
    Other OSes are not currently supported.

These instructions assume that:
  * `docker`, `kubectl`, and `gcloud` commands are installed.
  * `kubectl` has been configured and is authorized to push to a cluster


1. #### Make and push a docker image of Mixer with your custom adapter

   Execute the following command, replacing the `HUB` and `TAG` values with
   appropriate values for your environment:

   ```bash
   make HUB=<HUB> TAG=<TAG> push.docker.mixer
   ```
   
   If you have set `HUB` and `TAG` in a `~./profile` or a `.istiorc.mk` file, you 
   can omit those arguments from the command.

   This should produce output similar to:

   ```
   mkdir -p /usr/local/google/home/dougreid/go/out/linux_amd64/release/docker_temp
   cp docker/ca-certificates.tgz /usr/local/google/home/dougreid/go/out/linux_amd64/release/docker_temp
   bin/gobuild.sh /usr/local/google/home/dougreid/go/out/linux_amd64/release/mixs istio.io/istio/pkg/version ./mixer/cmd/mixs
   
   real	0m2.820s
   user	0m3.484s
   sys	0m0.556s
   cp /usr/local/google/home/dougreid/go/out/linux_amd64/release/mixs /usr/local/google/home/dougreid/go/out/linux_amd64/release/docker_temp
   time (cp mixer/docker/Dockerfile.mixer /usr/local/google/home/dougreid/go/out/linux_amd64/release/docker_temp/ && cd /usr/local/google/home/dougreid/go/out/linux_amd64/release/docker_temp && docker build -t gcr.io/istio-testing/mixer:dougreid -f Dockerfile.mixer .)
   Sending build context to Docker daemon  53.59MB
   Step 1/5 : FROM scratch
    ---> 
   Step 2/5 : ADD ca-certificates.tgz /
    ---> Using cache
    ---> 2fd8c1938ef6
   Step 3/5 : ADD mixs /usr/local/bin/
    ---> Using cache
    ---> 2be7030a6854
   Step 4/5 : ENTRYPOINT /usr/local/bin/mixs server
    ---> Using cache
    ---> d1e6674e0c9a
   Step 5/5 : CMD --configStoreURL=fs:///etc/opt/mixer/configroot --configStoreURL=k8s://
    ---> Using cache
    ---> c6061add6e00
   Successfully built c6061add6e00
   Successfully tagged gcr.io/istio-testing/mixer:dougreid
   
   real	0m0.421s
   user	0m0.020s
   sys	0m0.052s
   time (gcloud docker -- push gcr.io/istio-testing/mixer:dougreid)
   The push refers to a repository [gcr.io/istio-testing/mixer]
   bd5beecafe98: Layer already exists 
   40ce24ada7d0: Layer already exists 
   dougreid: digest: sha256:fe043cab14e4ac67aab2d7ec0047d40e6421223916d4576267e68daa7ba093df size: 739
   
   real	0m2.226s
   user	0m0.396s
   sys	0m0.100s
   ```
        
1. #### Generate the appropriate CRD for your custom adapter

   The Mixer binary has a utility for generating the Custom Resource Definitions
   for adapters. Invoke this utility as follows:
   
   ```bash
   $GOPATH/out/linux_amd64/release/mixs crd adapter
   ``` 

   Find the stanza for your custom adapter and save it to a file, named
   something like `custom-crd.yaml`.
   
   The `custom-crd.yaml` file you generate should look similar to:
   
   ```yaml
   kind: CustomResourceDefinition
   apiVersion: apiextensions.k8s.io/v1beta1
   metadata:
     name: stdios.config.istio.io
     labels:
       package: stdio
       istio: mixer-adapter
   spec:
     group: config.istio.io
     names:
       kind: stdio
       plural: stdios
       singular: stdio
     scope: Namespaced
     version: v1alpha2
   ```

1. #### Deploy your CRD

   Execute the following command:
   
   ```bash
   kubectl apply -f custom-crd.yaml
   ```
   
1. #### Edit the Mixer deployment configuration to reference your new image

   If you already have a Mixer instance running from a previous deployment of
   Istio, execute the following commands:
   
   1. Execute the following command to open the configuration for the Mixer deployment.
   
      ```bash
      kubectl -n istio-system edit deployment istio-mixer
      ```
      
   1. Change the `image` for the Mixer binary to match the tag for your new image.
   
      The `image` specification to change will look similar to:
      
      ```
      image: gcr.io/istio-testing/mixer:18a20f98c6e5d92817b9b00ed94c089f4e73aeec
      imagePullPolicy: IfNotPresent
      name: mixer
      ```
      
      Update `image` with the image tag for your image. Pay careful attention to
      the `imagePullPolicy` if you are attempting to reuse a tag. Exit and Save 
      the file.     
      
      The expected output is:
      
      ```bash
      deployment "istio-mixer" edited
      ```
      
   1. Please also update the Istio configuration specification (so that your 
      changes are preserved).
     
      Append the contents of `custom-crd.yaml` to `install/kubernetes/helm/istio/charts/mixer/templates/crds.yaml`.
      
      Be sure to a new label in the `metadata/labels` section that matches:
      ```yaml
      app: {{ template "mixer.name" . }}
      ```      
   
   1. Then regenerate the install artifacts via:   
      ```bash
      make generate_yaml
      ```
   
      You will need to update the `istio.yaml` (or similar) artifact to the image 
      you built in the previous steps.
   

   If you do not already have a Mixer instance running, deploy Istio as follows
   (assuming `istio.yaml` is the desired deployment):
   
   ```bash
   kubectl apply -f install/kubernetes/istio.yaml 
   ```
 
 1. #### Celebrate
 
    Woohoo! A new Mixer, built with your adapter code, should now be running in 
    your cluster. Configure a handler for your custom adapter and begin sending
    it instances!