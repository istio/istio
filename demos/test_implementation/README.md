This is a simple bookinfo application broken into four separate microservices setup as shown in the diagram below

![Bookinfo app](demos/example-app-bookinfo.png)

**NOTE: To run this demo on Minikube or any kubernetes cluster, you need kubernetes version 1.5.2 or higher.**. In minikube, you can set the kubernetes version using this command

```bash
minikube config set kubernetes-version v1.5.2
```

This demo has been tested with minikube version v0.15.0 (not guaranteed to work in older versions of minikube).

---

## Demo steps

#### Launch control plane

```bash
kubectl create -f controlplane.yaml
```

#### Launch the console version of bookinfo app

```bash
kubectl create -f bookinfo.yaml
```

Once the apps are up and running (check using `kubectl get po`), you can query the productpage by 

```bash
curl -s http://$(minikube ip):32000/productpage |json_pp
````

and you should see the productpage JSON of the form

```json
{
 "details":
  { 
    "isbn_10":"1234567890",
    "isbn_13":"123-1234567980",
    "language":"English",
    "paperback":"200 pages",
    "publisher":"PublisherA"
  },
  "reviews":{
     "reviewer1":{
        "text":"An extremely entertaining play by Shakespeare. The slapstick humour is refreshing!"
     },
     "reviewer2":{
        "text":"Absolutely fun and entertaining. The play lacks thematic depth when compared to other plays by Shakespeare."
     }
  }
}
```

_Note:_ If you don't have `json_pp` in your system, omit the `|json_pp`.
_Note:_ If you try the above command several times, you would notice that sometimes the output contains star ratings as well. This is because default versions are not set yet.

```json
{
 "details":
  { 
    "isbn_10":"1234567890",
    "isbn_13":"123-1234567980",
    "language":"English",
    "paperback":"200 pages",
    "publisher":"PublisherA"
  },
  "reviews":{
     "reviewer1":{
        "text":"An extremely entertaining play by Shakespeare. The slapstick humour is refreshing!",
        "rating":{
          "stars":5,
          "color":"black"
        }
     },
     "reviewer2":{
        "text":"Absolutely fun and entertaining. The play lacks thematic depth when compared to other plays by Shakespeare.",
        "rating":{
          "stars":4,
          "color":"black"
        }
     }
  }
}
```


#### Set `reviews-v1` as the default version for the reviews service.

```bash
kubectl create -f step1-default-route.yaml
```

Now, when you curl the productpage multiple times, you would only see the version without star ratings.

_NOTE_: After submitting each TPR (i.e. kubectl command), wait for a few seconds before curling the /productpage endpoint

#### Route traffic from `user=jason` to `reviews-v2` and rest to `reviews-v1`

```bash
kubectl create -f step2-single-user-testing.yaml
```

To test, try 

```bash
curl -s -b user=jason http://$(minikube ip):32000/productpage |json_pp
```

You should see that the JSON output has star ratings under reviews. Change the user name to something else and try again

```bash
curl -s -b user=shriram http://$(minikube ip):32000/productpage |json_pp
```

There would be no star ratings.

#### Fault Injection [NOT WORKING]

Lets inject a 7 second delay between `reviews-v2` and `ratings-v1` only for `user=jason`

```bash
kubectl create -f step3-fault-injection.yaml
```

To test,

```bash
time curl -b user=jason http://$(minikube ip):32000/productpage
```

The execution time should be about 6-7seconds

<!--- 
kubectl delete -f step3-fault-injection.yaml # Remove delay else you cant proceed to next step

`time curl -s -b user=jason http://$(minikube ip):32000/productpage` should show <1s execution time
--->


#### Rollout new version (`reviews-v3`) and send 25% of traffic to that pod

```bash
kubectl create -f step4-rollout-v3-25-percent.yaml
```

To test, run the following set of commands:

```bash
for i in `seq 1 100`; do curl -s http://$(minikube ip):32000/productpage >>a; echo "" >>a; done
cat a|sort|grep -c '"color":"red"'
```

The above commands store all outputs to a temporary file (a). We then grep for all responses that contain the red star rating and print the number of such responses.

The output should be 25 (i.e. 25/100 calls returned red star ratings)

#### Increase traffic to `reviews-v3` by sending 50% of traffic to the pod

```bash
kubectl create -f step5-rollout-v3-50-percent.yaml
```

To test, run the following commands

```bash
for i in `seq 1 100`; do curl -s http://$(minikube ip):32000/productpage >>b; echo "" >>b; done
cat b|sort|grep -c '"color":"red"'
```

You should see 50 (i.e. 50/100 calls returned red star ratings)

#### Send 100% of traffic to `reviews-v3`, completing the version upgrade

```bash
kubectl create -f step6-rollout-v3-100-percent.yaml
```

To test, run the following commands:

```bash
for i in `seq 1 100`; do curl -s http://$(minikube ip):32000/productpage >>c; echo "" >>c; done
cat c|sort|grep -c '"color":"red"'
```

You should see 100 (i.e. 100/100 calls returned red star ratings)
