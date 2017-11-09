# Deploying Mixer With Custom Adapters

Congratulations! You've managed to build a complete Mixer adapter and have validated
that it works via local testing.

Follow these steps to deploy a Mixer with your custom adapter to your service
mesh for use:
  * [Build a docker image of Mixer with your custom adapter](#build-a-docker-image-of-mixer-with-your-custom-adapter)
  * [Tag your new Mixer image appropriately for your image registry of choice](#tag-your-new-mixer-image-appropriately-for-your-image-registry-of-choice)
  * [Push your new Mixer image into the image registry](#push-your-new-mixer-image-into-the-image-registry)
  * [Generate the appropriate CRD for your custom adapter](#generate-the-appropriate-crd-for-your-custom-adapter)
  * [Deploy your CRD](#deploy-your-crd)
  * [Edit the Mixer deployment configuration to reference your new image](#edit-the-mixer-deployment-configuration-to-reference-your-new-image)
  * [Celebrate](#celebrate)

NOTE: All commands should be executed from the root of the istio/istio repository.

These instructions assume that:
  * `bazel` is installed
  * `docker`, `kubectl`, and `gcloud` commands are installed.
  * `kubectl` has been configured and is authorized to push to a cluster

1. Build a docker image of Mixer with your custom adapter.

   Execute the following command:

   ```bash
   bazel run //mixer/docker:mixer
   ```

   This should produce output similar to:

   ```
   INFO: Running command line: bazel-bin/mixer/docker/mixer
   Loaded image ID: sha256:f2eb3e4f7f98a8ff1a9fa92666e398b102d28da877711f93a7d87e60e692ac8f
   Tagging f2eb3e4f7f98a8ff1a9fa92666e398b102d28da877711f93a7d87e60e692ac8f as istio/mixer/docker:mixer
   ```

   Confirm this image is available via:

   ```bash
   docker images istio/mixer/docker:mixer
   ```

   The expected output is:

   ```
   REPOSITORY           TAG                 IMAGE ID            CREATED             SIZE
   istio/mixer/docker   mixer               f2eb3e4f7f98        47 years ago        169.3 MB
   ```

1. Tag your new Mixer image appropriately for your image registry of choice.

   If using Google Cloud Registry, use a command similar to:
   
   ```bash
   docker tag istio/mixer/docker:mixer gcr.io/$PROJECT/mixer:$DOCKER_TAG   
   ```
   
   Set `$PROJECT` to your project id and `$DOCKER_TAG` to your desired tag before executing the command.
   
1. Push your new Mixer image into the image registry.

   If using Google Cloud Registry, use a command similar to:
   
   ```bash
   gcloud docker -- push gcr.io/$PROJECT/mixer:$DOCKER_TAG
   ```
   
1. Generate the appropriate CRD for your custom adapter.

   The Mixer binary has a utility for generating the Custom Resource Definitions for adapters. Invoke this utility as follows:
   
   ```bash
   bazel run mixer/cmd/server:mixs -- crd adapter
   ``` 

   Find the stanza for your custom adapter and save it to a file, named something like `custom-crd.yaml`.
   
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

1. Deploy your CRD.

   Execute the following command:
   
   ```bash
   kubectl apply -f custom-crd.yaml
   ```
   
1. Edit the Mixer deployment configuration to reference your new image.

   If you already have a Mixer instance running from a previous deployment of Istio, execute the following commands:
   
   1. Execute the following command to open the configuration for the Mixer deployment.
   
      ```bash
      kubectl -n istio-system edit deployment istio-mixer
      ```
      
   1. Change the `image` for the Mixer binary to match the tag for your new image.
   
      The `image` specification to change will look similar to:
      
      ```
      image: gcr.io/istio-testing/mixer:5253b6b574a98b209c0ef3d0d6e90c1b8d6a5c2a
      imagePullPolicy: IfNotPresent
      name: mixer
      ```
      
      Update `image` with the image tag for your image. Exit and Save the file.
      
      The expected output is:
      
      ```bash
      deployment "istio-mixer" edited
      ```
      
   Please also update the Istio configuration specification to reference your image (so that your changes are preserved).
     
   Edit `install/kubernetes/templates/istio-mixer.yaml.tmpl` as follows:
   * Update the `image` specification in the Mixer deployment stanza
   * Append the contents from `custom-crd.yaml` to the end of the yaml file. 
   
   Then regenerate `istio.yaml` via:   
   ```bash
   install/updateVersion.sh
   ```

   If you do not already have a Mixer instance running, deploy Istio as follows (assuming `istio.yaml` is the desired deployment):
   
   ```bash
   kubectl apply -f install/kubernetes/istio.yaml 
   ```
 
 1. Celebrate
 
    Woohoo! A new Mixer, built with your adapter code, should now be running in your cluster. Configure a handler for your custom adapter and begin sending it instances!