---
title: Glossary
headline: Glossary
sidenav: doc-side-reference-nav.html
bodyclass: docs
layout: docs
type: markdown

---
<div class="container">
    <div class="row">
        <div class="col-md-11 nofloat center-block">
      <!-- Constrain readable sections to 9 unit wide columns for improved readability. -->
            <div class="col-md-9 glossary-container">
                <h5 class="glossary-title">Glossary</h5>
                <ul class="list-unstyled">
                    <li class="submenu">
                        <h6 class="arrow-r">Mixer</h6>
                        <div class="submenu-content">
                            <p>TBD</p>
                        </div>
                    </li>

                    <li class="submenu">
                        <h6 class="arrow-r">Service Mesh</h6>
                        <div class="submenu-content">
                            <p>TBD</p>
                        </div>
                    </li>

                    <li class="submenu">
                        <h6 class="arrow-r">Microservice</h6>
                        <div class="submenu-content">
                            <p>TBD</p>
                        </div>
                    </li>
                    
<!-- Words to add to the glossary

Service instance

Service version

Source - ?

Destination -- a fully-qualified hostname, some tags, a Load balancing policy, a Circuit breaker policy,
a Timeout policy, a Retry policy, a L7 fault injection policy, a L4 fault injection policy, and
"Custom policy implementations"

RouteRule - destination, MatchCondition, 0..N DestinationWeights, precedence

ProxyMeshConfig -- nothing

Load balancing policy -- ROUND_ROBIN|LEAST_CONN|RANDOM|Any

Circuit breaker policy -- Currently a bunch of threshold parameters, with a work item to support all Envoy capabilities

Timeout policy -- seconds (a double), plus a feature to let downstream service specify via a header (!?!), plus "custom"

Retry policy -- # of attempts, plus a feature to let downstream service specify via a header (!?!), plus "custom"

L7 fault injection policy -- Delay fault, Abort fault, plus some tags to trigger them on specific header patterns

L4 fault injection policy -- bandwidth Throttle, TCP terminate connection

MatchCondition -- source, (source) tags, TCP L4MatchAttributes, UDP L4MatchAttributes, "Set of HTTP match conditions"

DestinationWeight -- fully-qualified destination, tags, and weight (the sum of weights "across destination" should add up to 100). (Or do we mean RFC 2119 style "SHOULD" for "should)?

L4MatchAttributes just 0..N source and destination subnet strings, of the forms a.b.c.d and a.b.c.d/xx

HTTP match conditions -- This seems to be HTTP and gRPC headers ... the examples given are "uri", "scheme", "authority", and we
match them case-insensitive, and using exact|prefix|regexp format

Delay fault -- fixed or exponential delay. Fixed has a duration plus a % of requests to delay. Exponential has a "mean" (that I don't understand)

Abort fault -- A type plus % of requests to abort. The types are only HTTP, HTTP/2, gRPC. No TCP resets or TLS (?!?)

Istio Manager

Istio Mixer

Istio Proxy

Proxy Mesh

Upstream

CDS Cluster Discovery Service -- See https://lyft.github.io/envoy/docs/configuration/cluster_manager/cds.html?highlight=cds#cluster-discovery-service

SDS Service Discovery Service -- See https://lyft.github.io/envoy/docs/intro/arch_overview/service_discovery.html#arch-overview-service-discovery-sds

RDS Route Discovery Service -- See https://lyft.github.io/envoy/docs/configuration/http_conn_man/rds.html#route-discovery-service

-->
                </ul>
            </div>
        </div>
    </div>
</div>
