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

You can query the productpage by 

`curl -sH "Host: productpage:9080" http://$(minikube ip):32000/productpage`

 and you should see the productpage JSON

If you want pretty printed output, use json_pp (needs installation)

`curl -sH "Host: productpage:9080" http://192.168.99.100:32000/productpage |json_pp`

then use the manager CLI to add rules..
kubectl create -f tpr-step1-default-route.yaml #send to reviews-v1 by default

kubectl create -f tpr-step2-single-user-testing.yaml #send Cookie: user=jason to reviews-v2

`curl -s -H "Host: productpage:9080" -b user=jason http://192.168.99.100:32000/productpage |json_pp` should show star ratings
`curl -s -H "Host: productpage:9080" -b user=shriram http://192.168.99.100:32000/productpage |json_pp` should show no star ratings

# Dont try step3. Fault injection is broken
kubectl create -f tpr-step3-fault-injection.yaml # Inject 7s delay to ratings

`time curl -s -H "Host: productpage:9080" -b user=jason http://192.168.99.100:32000/productpage` should show 6-7s execution time

kubectl delete -f tpr-step3-fault-injection.yaml # Remove delay else you cant proceed to next step

`time curl -s -H "Host: productpage:9080" -b user=jason http://192.168.99.100:32000/productpage` should show <1s execution time

kubectl create -f tpr-step4-rollout-v3-25-percent.yaml # send 25% to reviews-v3

for i in `seq 1 100`; do curl -s -H "Host: productpage:9080" http://192.168.99.100:32000/productpage >>a; echo "" >>a; done
cat a|sort|grep -c '"color":"red"'

should show 25 (i.e. 25/100 calls returned red star ratings)

kubectl create -f tpr-step5-rollout-v3-50-percent.yaml # send 50% to reviews-v3

for i in `seq 1 100`; do curl -s -H "Host: productpage:9080" http://192.168.99.100:32000/productpage >>b; echo "" >>b; done
cat b|sort|grep -c '"color":"red"'

should show 50 (i.e. 50/100 calls returned red star ratings)

kubectl create -f tpr-step6-rollout-v3-100-percent.yaml # send 100% to reviews-v3

for i in `seq 1 100`; do curl -s -H "Host: productpage:9080" http://192.168.99.100:32000/productpage >>c; echo "" >>c; done
cat c|sort|grep -c '"color":"red"'

should show 100 (i.e. 100/100 calls returned red star ratings)

NOTE: After submitting each TPR (i.e. kubectl command), wait for a few seconds before curling the /productpage endpoint
