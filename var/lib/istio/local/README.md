Directory included in the docker image, will be used by Galley to load local config overrides.
Configs for Istio pilot or other local overrides can be added here. 

You can create a derived docker image, extending the base 'istiod' and adding more configs.
Or you can use it in k8s or docker, mounting a volume or config map to this path.
