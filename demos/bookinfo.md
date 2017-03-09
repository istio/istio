In this demo, we will deploy a simple app that displays information about a
book, similar to a single catalog entry of an online book store. Displayed
on the page is a description of the book, book details (ISBN, number of
pages, and so on), and a few book reviews.

The bookinfo application is broken into four separate microservices:

* *productpage*. The productpage microservice calls the *details* and *reviews* microservices to populate the page.
* *details*. The details microservice contains book information.
* *reviews*. The reviews microservice contains book reviews. It also calls the *ratings* microservice.
* *ratings*. The ratings microservice contains book ranking information that accompanies a book review. 

There are 3 versions of the reviews microservice:

* Version v1 doesn't call the ratings service.
* Version v2 calls the ratings service, and displays each rating as 1 to 5 black stars.
* Version v3 calls the ratings service, and displays each rating as 1 to 5 red stars.

The end-to-end architecture of the application is shown below.

![Bookinfo app](example-app-bookinfo.png)

This application is polyglot, i.e., the microservices are written in
different languages. All microservices are packaged with an
Istio sidecar that manages all incoming and outgoing calls for the service.

<!-- > Note: the following instructions assume that your current working directory -->
<!-- > is [apps/bookinfo](apps/bookinfo). -->

*CLI*: This walkthrough will use the _istioctl_ CLI that provides a
convenient way to apply routing rules and policies for upstreams. The
`demos/` directory has three binaries: `istioctl-osx`, `istioctl-windows`,
`istioctl-linux` targeted at Mac, Windows and Linux users
respectively. Please download the tool appropriate to your platform and
rename the tool to `istioctl`. For example:

```bash
$ cp istioctl-osx /usr/local/bin/istioctl
```

## Running the Bookinfo Application

1. Bring up the control plane:

   ```bash
   $ kubectl create -f apps/controlplane.yaml
   ```
   
   This command launches the istio manager and an envoy-based ingress controller, which will be used
   to implement the gateway for the application. 

1. Bring up the application containers:

   ```bash
   $ kubectl create -f apps/bookinfo/bookinfo-istio.yaml
   ```

   The above command creates the gateway ingress resource and launches the 4 microservices as described
   in the diagram above. The reviews microservice has 3 versions: v1, v2, and v3.
   Note that in a realistic deployment, new versions of a microservice are deployed over 
   time instead of deploying all versions simultaneously.
   
1. Confirm that all services and pods are correctly defined and running:

   ```bash
   $ kubectl get services
   NAME                       CLUSTER-IP   EXTERNAL-IP   PORT(S)        AGE
   details                    10.0.0.192   <none>        9080/TCP       34s
   istio-ingress-controller   10.0.0.74    <nodes>       80:32000/TCP   1m
   istio-route-controller     10.0.0.144   <none>        8080/TCP       1m
   kubernetes                 10.0.0.1     <none>        443/TCP        6h
   productpage                10.0.0.215   <none>        9080/TCP       34s
   ratings                    10.0.0.75    <none>        9080/TCP       34s
   reviews                    10.0.0.113   <none>        9080/TCP       34s
   ```

   and

   ```bash
   $ kubectl get pods
   NAME                                        READY     STATUS    RESTARTS   AGE
   details-v1-2834985933-31gns                 2/2       Running   0          41s
   istio-ingress-controller-1035658521-3ztkr   1/1       Running   0          1m
   istio-route-controller-3817920337-80753     1/1       Running   0          1m
   productpage-v1-1157331189-7tsh1             2/2       Running   0          41s
   ratings-v1-2039116803-k7kr8                 2/2       Running   0          41s
   reviews-v1-2171892778-57jt6                 2/2       Running   0          41s
   reviews-v2-2641065004-hfh54                 2/2       Running   0          41s
   reviews-v3-3110237230-3trfv                 2/2       Running   0          41s
   ```

1. Determine the Gateway ingress URL (TEMPORARY - instruction subject to change)

   Determine the node on which the `gateway` (ingress controller) runs, and use the node's IP address
   as the external gateway IP.

   ```bash
   $ kubectl describe pod istio-ingress-controller-1035658521-3ztkr | grep Node
   Node:		minikube/192.168.99.100
   $ export GATEWAY_URL=192.168.99.100:32000
   ```

### Content Based Routing



Since we have 3 versions of the reviews microservice running, we need to set the default route.
Otherwise if you access the application several times, you would notice that sometimes the output contains 
star ratings. This is because without an explicit default version set, Istio will 
route requests to all available versions of a service in a random fashion.

1. Set the default version for all microservice to v1. 

   ```bash
   $ istioctl create route-rule productpage-default -f apps/bookinfo/route-rule-productpage-v1.yaml; \
     istioctl create route-rule reviews-default -f apps/bookinfo/route-rule-reviews-v1.yaml; \
     istioctl create route-rule ratings-default -f apps/bookinfo/route-rule-ratings-v1.yaml; \
     istioctl create route-rule details-default -f apps/bookinfo/route-rule-details-v1.yaml
   ```

   You can display the routes that are defined with the following command:

   ```bash
   $ istioctl list route-rule
   kind: route-rule
   name: ratings-default
   namespace: default
   spec:
     destination: ratings.default.svc.cluster.local
     precedence: 1
     route:
     - tags:
         version: v1
       weight: 100
   ---
   kind: route-rule
   name: reviews-default
   namespace: default
   spec:
     destination: reviews.default.svc.cluster.local
     precedence: 1
     route:
     - tags:
         version: v1
       weight: 100
   ---
   kind: route-rule
   name: details-default
   namespace: default
   spec:
     destination: details.default.svc.cluster.local
     precedence: 1
     route:
     - tags:
         version: v1
       weight: 100
   ---
   kind: route-rule
   name: productpage-default
   namespace: default
   spec:
     destination: productpage.default.svc.cluster.local
     precedence: 1
     route:
     - tags:
         version: v1
       weight: 100
   ---
   ```

   > Note: In the current Kubernetes implemention of Istio, the rules are stored in ThirdPartyResources.
   > You can look directly at the stored rules in Kubernetes using the `kubectl` command. For example,
   > the following command will display all defined rules:
   > ```bash
   > $ kubectl get istioconfig -o yaml
   > ```

   Since rule propagation to the proxies is asynchronous, you should wait a few seconds for the rules
   to propagate to all pods before attempting to access the application.

   If you open the Bookinfo URL (`http://$GATEWAY_URL/productpage`) in your browser,
   you should see the bookinfo application `productpage` displayed. Notice that the `productpage`
   is displayed, with no rating stars since `reviews:v1` does not access the ratings service.

1. Route a specific user to `reviews:v2`

   Lets enable the ratings service for test user "jason" by routing productpage traffic to
   `reviews:v2` instances.

   ```bash
   $ istioctl create route-rule reviews-test-v2 -f apps/bookinfo/route-rule-reviews-test-v2.yaml 
   ```

   Confirm the rule is created:

   ```bash
   $ istioctl get route-rule reviews-test-v2
   destination: reviews.default.svc.cluster.local
   match:
     http:
       Cookie:
         regex: ^(.*?;)?(user=jason)(;.*)?$
   precedence: 2
   route:
   - tags:
       version: v2
   ```

   Log in as user "jason" at the `productpage` web page. You should now see ratings (1-5 stars) next
   to each review.

### Fault Injection

   To test our bookinfo application microservices for resiliency, we will _inject a 7s delay_
   between the reviews:v2 and ratings microservices. Since the _reviews:v2_ service has a
   10s timeout for its calls to the ratings service, we expect the end-to-end flow to
   continue without any errors.

1. Inject the delay

   Create a fault injection rule, to delay traffic coming from user "jason" (our test user).

   ```bash
   $ istioctl create route-rule ratings-test-delay -f apps/bookinfo/destination-ratings-test-delay.yaml
   ```

   Confirm the rule is created:

   ```bash
   $ istioctl get route-rule ratings-test-delay
   destination: ratings.default.svc.cluster.local
   httpFault:
     delay:
       fixedDelaySeconds: 7
       percent: 100
   match:
     http:
       Cookie:
         regex: "^(.*?;)?(user=jason)(;.*)?$"
   precedence: 2
   route:
   - tags:
       version: v1
   ```

   Allow several seconds to account for rule propagation delay to all pods.

1. Observe application behavior

   If the application's front page was set to correctly handle delays, we expect it
   to load within approximately 7 seconds. To see the web page response times, open the
   *Developer Tools* menu in IE, Chrome or Firefox (typically, key combination _Ctrl+Shift+I_
   or _Alt+Cmd+I_) and reload the `productpage` web page.

   You will see that the webpage loads in about 6 seconds. The reviews section will show
   *Sorry, product reviews are currently unavailable for this book*.

   The reason that the entire reviews service has failed is because our bookinfo application
   has a bug. The timeout between the productpage and reviews service is less (3s + 1 retry = 6s total)
   than the timeout between the reviews and ratings service (10s). These kinds of bugs can occur in
   typical enterprise applications where different teams develop different microservices
   independently. Istio's fault injection rules help you identify such anomalies without
   impacting end users.

   > Notice that we are restricting the failure impact to user "jason" only. If you login
   > as any other user, you would not experience any delays.

### Fixing the bug

At this point we would normally fix the problem by either increasing the
productpage timeout or decreasing the reviews to ratings service timeout,
terminate and restart the fixed microservice, and then confirm that the `productpage`
returns its response without any errors.
(Left as an exercise for the reader - change the delay rule to
use a 2.8 second delay and then run it against the v3 version of reviews.)

However, we already have this fix running in v3 of the reviews service, so
we can next demonstrate deployment of a new version.

### Gradually migrate traffic to reviews:v3 for all users

Now that we have tested the reviews service, fixed the bug and deployed a
new version (`reviews:v3`), lets route all user traffic from `reviews:v1`
to `reviews:v3` in two steps.

First, transfer 50% of traffic from `reviews:v1` to `reviews:v3` with the following command:

```bash
   $ istioctl update route-rule reviews-default -f apps/bookinfo/route-rule-reviews-50-v3.yaml
```

> Notice that we are using `istioctl update` instead of `create`.

To see the new version you need to either Log out as test user "jason" or delete the test rules
that we created exclusively for him:

```bash
   $ istioctl delete route-rule reviews-test-v2
   $ istioctl delete route-rule ratings-test-delay
```

You should now see *red* colored star ratings approximately 50% of the time when you refresh
the `productpage`.

> Note: With the Envoy sidecar implementation, you may need to refresh the `productpage` 100 times
> to see the proper distribution.

When we are confident that our Bookinfo app is stable, we route 100% of the traffic to `reviews:v3`:

```bash
   $ istioctl update route-rule reviews-default -f apps/bookinfo/route-rule-reviews-v3.yaml
```

You can now log in to the `productpage` as any user and you should always see book reviews
with *red* colored star ratings for each review.

## Cleanup

1. Delete the routing rules and terminate the application and control plane pods

   ```bash
   $ ./apps/bookinfo/cleanup.sh
   ```

1. Confirm shutdown

   ```bash
   $ istioctl list route-rule   #-- there should be no more routing rules
   $ istioctl list destination  #-- there should be no more policy rules
   $ kubectl get pods           #-- the bookinfo and control plane services should be deleted
   No resources found.
   ```
