Enable ingress in your minikube cluster

minikube addons enable ingress

launch the control plane
kubectl create -f controlplane.yaml

---
This is a simple bookinfo application broken into four separate microservices:

productpage. The productpage microservice calls the details and reviews microservices to populate the page.

details. The details microservice contains book information.

reviews. The reviews microservice contains book reviews. It also calls the ratings microservice.

ratings. The ratings microservice contains book ranking information that accompanies a book review.

There are 3 versions of the reviews microservice:

Version v1 doesn’t call the ratings service.
Version v2 calls the ratings service, and displays each rating as 1 to 5 black stars.
Version v3 calls the ratings service, and displays each rating as 1 to 5 red stars.

To compile the apps, do
make build.productpage build.ratings build.reviews build.details

To create docker containers
make dockerize.productpage ... and so on. see Makefile for more targets.

Push these images to your dockerhub and change bookinfo.yaml to point to your dockerhub images accordingly.

For e.g., replace the docker.io/rshriram/go-bookinfo-productpage-v1 with the appropriate image name

Once the bookinfo.yaml file is setup, you can launch the application in a minikube cluster with a simple

kubectl create -f bookinfo.yaml

You can query the productpage by curl http://$(minikube ip)/productpage and you should see the productpage JSON

then use the manager CLI to add rules..
cat step1-...|manager config put route-rule default-route #send to reviews-v1 by default
cat step2-...|manager config put route-rule content-route #send Cookie: user=jason to reviews-v2
cat step3-...|manager config put destination fault-injection # Inject 7s delay to ratings for user=jason (not sure if this works)
cat step4-...|manager config put route-rule weighted-route-25 # send 25% to reviews-v3
cat step5-...|manager config put route-rule weighted-route-50 # send 50% to reviews-v3
cat step6-...|manager config put route-rule weighted-route-100 # send 100% to reviews-v3
