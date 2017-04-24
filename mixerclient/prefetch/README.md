
## Introduction

A c++ library for a rate limiting prefetch algorithm.

The rate limiting feature is for a system wanted to limit request rate. For example, a proxy wants to limit request rate to protect the backend server. The exceeded requests will be rejected by the proxy and will not reach the backend server.

If a system has multiple proxy instances, rate limiting could be local or global. If local, each running instance enforces its own limit.  If global, all running instances are subjected to a global limit.  Global rate limiting is more useful than local.  For global rate limiting, usually there is a rate limiting server to enforce the global limits, each proxy instance needs to call the server to check the limits.

If each proxy instance is calling the rate limiting server for each request it is processing, it will greatly increase the request latency by adding a remote call. It is a good idea for the proxy to prefetch some tokens so not every request needs to make a remote call to check rate limits.

Here presents a prefetch algorithm for that purpose.

This code presents a rate limiting prefetch algorithm. It can achieve:
* All rate limiting decisions are done at local, not need to wait for remote call.
* It works for both big rate limiting window, such as 1 minute, or small window, such as 1 second.


## Algorithm

Basic idea is:
* Use a predict window to count number of requests, use that to determine prefetch amount.
* There is a pool to store prefetch tokens from the rate limiting server.
* When the available tokens in the pool is less than half of desired amount, trigger a new prefetch.
* If a prefetch is negative (requested amount is not fully granted), need to wait for a period time before next prefetch.

There are three parameters in this algorithm:
* predictWindow: the time to count the requests, use that to determine prefetch amount
* minPrefetch: the minimum prefetch amount
* closeWaitWindow: the wait time for the next prefetch if last prefetch is negative.

