# Istio Demos

This directory contains a stripped down version of the Amalgam8 demo applications (a simple helloworld app and a bookinfo app). The intent is to use this as the starting point for the outline of one or more Istio demos highlighting features such as ACLs, rate limiting, routing, etc. The Istio demo is intended to serve as the first milestone for the project (end of March) as well as serve as a checkpoint for functional parity with Amalgam8. 

## Outline of Milestone 1 Demo

This is a rough outline of the demo. It is based on the bookinfo application in [apps/bookinfo](apps/bookinfo).
Once finalized, it will be described in detail in [bookinfo.md](bookinfo.md), which is currently still rough around the edges. Here are the key steps:

1. **Seamless integration** Show that users can take an *unmodified* app (composed of services) and drop it into Istio. It should work out of the box with the Istio Service Mesh.

   We provide instuctions to run the bookinfo services (`productpage`, `details` and `reviews-v1`), which will be coded to call each other 
   directly using DNS names (e.g., `productpage` talks to `reviews.bookinfo` and `details.bookinfo`). *The vanilla app will not be using 
   any proxy*. To simulate an existing (non-Istio) environment, we will include a docker compose file with hardcoded hostnames so people 
   can run it standalone using docker-compose. For example,

    ```
    productpage.bookinfo:
     - links:
        -details.bookinfo
        -reviews.bookinfo
    ```

   The next step would be to take the exact same container images, use Istio config files and start up the app in kubernetes. The config 
   file should be simple enough and prove that there is nothing needed on the end user's behalf to migrate the app to Istio. The Istio 
   version of the app would be running with Envoy as the service mesh.
 
   *Now we add a new version of `reviews` service called `reviews-v2`, which calls a new "third party" service called `ratings`.*

1. **Content-based routing w/ ACLs:** Lets say everyone in our US branch (`@us.squirrel.com`) can potentially access this reviews 
service. We set a routing rule (e.g., `Cookie: .+?(user=([a-zA-z]+)@us.squirrel.com)`) to route traffic from US branch to `reviews-v2`. 
Other users, including those outside the organization, continue to see `reviews-v1`.

 * Since we are doing internal testing, we'll set an ACL to make sure that only devs with role `Tester` can access the `reviews-v2` 
   service. In our case, users `Chipmunk` and `Marmot` are the only ones with `Tester` access. Others devs in our company
   (Red, Plantain) will not be able to call `reviews-v2`. We will also show how ACL checking works by logging in as a `Plantain` 
   and show that the reviews service calls are blocked.
   
 * We will also show that users outside the us.squirrel.com domain (say `Prairie` from eu.squirrel.com) still see `reviews-v1`.

1. **Systematic Fault injection** Now we demonstrate how to test the resilience of the new service by injecting faults and testing the end 
to end functionality.

   We will set the Istio config to insert a 5 second delay between the `reviews-v2` and `ratings` microservices and then return a HTTP 429 
   (*too many requests*) to indicate that the third party Ratings service was throttling our API calls and finally denied our requests as 
   we exceeded the free quota. However, the user ends up seeing this backend error (entire reviews section in webpage is gone).
   We'll explain how the application should have behaved and point out how this has uncovered a bug.

1. **Incremental rollout using % traffic split**  After presumably fixing the bug (not shown) in `reviews-v2` in a new v3 version,
we now use Istio to do a gradual rollout to public, by shifting traffic to 
`reviews-v3`. We start by sending 10% of traffic to `reviews-v3`, and then setup config that increases traffic
to 20%, 50%, and finally 100%.

 * *Highlight the difference between what is available in kubernetes and other systems today vs what Istio does.* Explicitly call 
   out/show (`kubectl list po`) the fact that traffic split and instance scaling are decoupled thanks to the service mesh.

1. **Rate Limiting:**  * Since `ratings` is an external service for which we are paying (like going to rotten tomatoes),
we set a rate limit on the `ratings` service such that the load remains under the Free quota (100q/s). We now
launch a simple load gen on the webpage and see that beyond 100 req/s, we stop seeing stars.
