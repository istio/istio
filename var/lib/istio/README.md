This is the base of all istio configs.

On VMs, the .deb file creates a user 'istio' with HOME set to /var/lib/istio. 
The docker image will have the same structure - with the directory owned by istio user, to avoid any root access.

Files will be included in the base docker image for istiod and in the .deb file, reflecting the
recommended default configuration for istiod. 

The files are different from the ones used in the old installer - use of helm and unused options
and additional cleanup is performed.

User can edit the files or mount its own volume in docker, and in case of K8S ConfigMaps can be used to replace
any of the directories.  
