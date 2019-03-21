Evaluated options:

# KNative build

This is likely the best long term option. 

#  CircleCI
 
Anything running docker is limitted to either 'remote docker' or machine. Both are 2CPU/8G. 

I think the only viable options are:

- use minikube root, as in istio/istio
- use circle just to drive a remote k8s cluster

What doesn't work:
- use 'machine' with KIND for the testing: golang version in machine is too old 
- Remote-docker doesn't seem to have any benefits compared with machine from this perspective.
There may be a hacky way to get it to work - by running the installer as a kubernetes pod or in 
a separate docker sharing the KIND docker network space.  


# Drone.io

Another open source CI with pretty good docker integration, it also runs in k8s.

# Concourse 

Open source, k8s support. 
