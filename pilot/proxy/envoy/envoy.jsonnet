local services = import "services.json";
local instances = import "instances.json";
local context = {
    domain: "default.svc.cluster.local",
};

local model = {
    key(hostname, labels, port_desc)::
        local labels_strings = ["%s=%s" % [key, labels[key]] for key in std.objectFields(labels)];
        "%s|%s|%s" % [hostname, port_desc.name, std.join(",", std.sort(labels_strings))],

    is_http2(protocol)::
        protocol == "HTTP2",

    is_http(protocol)::
        protocol == "HTTP" || self.is_http2(protocol),
};

local config = {
    inbound_cluster(port, protocol)::
        {
            name: "in.%d" % [port],
            type: "STATIC",
            lb_policy: "ROUND_ROBIN",
            hosts: [{
                socket_address: {
                    protocol: "TCP",
                    address: "127.0.0.1",
                    port_value: port,
                },
            }],
            [if model.is_http2(protocol) then "http2_protocol_options"]: {},
            hostname:: "",
        },

    outbound_cluster(hostname, labels, port_desc)::
        local key = model.key(hostname, labels, port_desc);
        {
            name: "out.%s" % [std.md5(key)],
            type: "EDS",
            eds_cluster_config: {
                service_name: key,
                eds_config: { ads: {} },
            },
            lb_policy: "ROUND_ROBIN",
            hostname:: hostname,
        },

    default_route(cluster, operation)::
        {
            match: {
                prefix: '/',
            },
            route: {
                cluster: cluster.name,
            },
            decorator: {
                operation: operation,
            },
        },

    inbound_listeners(instances)::
        [{
            local protocol = instance.endpoint.service_port.protocol,
            local port = instance.endpoint.port,
            local cluster = config.inbound_cluster(port, protocol),
            local prefix = "in_%s_%d" % [protocol, port],
            name: "in_%s_%s_%d" % [protocol, instance.endpoint.ip_address, port],
            cluster:: cluster,
            address: {
                socket_address: {
                    protocol: "TCP",
                    address: instance.endpoint.ip_address,
                    port_value: port,
                },
            },
            filter_chains: [
                {
                    filters: [
                        if model.is_http(protocol) then
                            {
                                name: "envoy.http_connection_manager",
                                config: {
                                    stat_prefix: prefix,
                                    codec_type: "AUTO",
                                    route_config: {
                                        name: prefix,
                                        virtual_hosts: [{
                                            name: prefix,
                                            domains: ["*"],
                                            routes: [config.default_route(cluster, "inbound_route")],
                                        }],
                                    },
                                    http_filters: [{
                                        name: "envoy.router",
                                        config: {
                                            deprecated_v1: true,
                                        },
                                    }],
                                },
                            }
                        else
                            {
                                name: "envoy.tcp_proxy",
                                config: {
                                    stat_prefix: prefix,
                                    cluster: cluster.name,
                                },
                            },
                    ],
                },
            ],
        } for instance in instances],

    outbound_http_ports(services)::
        std.set([
            port.port
            for service in services
            for port in service.ports
            if model.is_http(port.protocol)
        ]),

    longest_suffix(a, b, j)::
        if j >= std.length(a) || j >= std.length(b) then
            j
        else if a[std.length(a) - 1 - j] != b[std.length(b) - 1 - j] then
            j
        else
            config.longest_suffix(a, b, j + 1),

    domains(service, port)::
        local service_names = std.split(service.hostname, ".");
        local context_names = std.split(context.domain, ".");
        local j = config.longest_suffix(service_names, context_names, 0);
        local expansions = [
            std.join(".", service_names[0:std.length(service_names) - i])
            for i in std.range(0, j)
        ] + [service.address];
        expansions + ["%s:%d" % [host, port] for host in expansions],

    // special value: port = 0 means all ports
    outbound_http_routes(services, port)::
        {
            name: "%d" % [port],
            virtual_hosts: [
                {
                    name: "%s:%d" % [service.hostname, port_desc.port],
                    cluster:: config.outbound_cluster(service.hostname, {}, port_desc),
                    domains: config.domains(service, port_desc.port),
                    routes: [
                        config.default_route(self.cluster, "default_route"),
                    ],
                }
                for service in services
                for port_desc in service.ports
                if model.is_http(port_desc.protocol) && (port == 0 || port_desc.port == port)
            ],
        },

    outbound_listeners(services)::
        [
            {
                local prefix = "out_%s_%s_%d" % [port.protocol, service.hostname, port.port],
                local cluster = config.outbound_cluster(service.hostname, {}, port),
                name: prefix,
                cluster:: cluster,
                address: {
                    socket_address: {
                        protocol: "TCP",
                        address: service.address,
                        port_value: port.port,
                    },
                },
                filter_chains: [
                    {
                        filters: [
                            {
                                name: "envoy.tcp_proxy",
                                config: {
                                    stat_prefix: prefix,
                                    cluster: cluster.name,
                                },
                            },
                        ],
                    },
                ],
            }
            for service in services
            for port in service.ports
            if !model.is_http(port.protocol)
        ] + [
            {
                local prefix = "out_HTTP_%d" % [port],
                name: prefix,
                address: {
                    socket_address: {
                        protocol: "TCP",
                        address: "0.0.0.0",
                        port_value: port,
                    },
                },
                filter_chains: [
                    {
                        filters: [
                            {
                                name: "envoy.http_connection_manager",
                                config: {
                                    stat_prefix: prefix,
                                    codec_type: "AUTO",
                                    rds: {
                                        config_source: { ads: {} },
                                        route_config_name: "%d" % [port],
                                    },
                                    http_filters: [{
                                        name: "envoy.router",
                                        config: { deprecated_v1: true },
                                    }],
                                },
                            },
                        ],
                    },
                ],
            }
            for port in config.outbound_http_ports(services)
        ],

    virtual_listener(port)::
        {
            name: "virtual",
            address: {
                socket_address: {
                    protocol: "TCP",
                    address: "0.0.0.0",
                    port_value: port,
                },
            },
            use_original_dst: true,
            filter_chains: [],
        },

    sidecar_listeners(instances, services)::
        [
            listener { deprecated_v1+: { bind_to_port: false } }
            for listener in config.inbound_listeners(instances) + config.outbound_listeners(services)
        ],
};

{
    listeners: [config.virtual_listener(15000)] +
               config.sidecar_listeners(instances, services),
    routes: config.outbound_http_routes(services, 0),
    clusters: [
        listener.cluster
        for listener in self.listeners
        if "cluster" in listener
    ],
}
