import "lists"

servers: [...server] & lists.MinItems(1)

server: {
    hosts: [...host] & lists.MinItems(1)

    host: "^(\*|[-a-z-A-Z0-9]*[a-zA-Z0-9])(\.[-a-z-A-Z0-9]*[a-zA-Z0-9])*$"

    port: {
        number: uint32

        protocol: [ "HTTP" | "HTTPS" | "GRPC" | "HTTP2" | "MONGO" | "TCP" | "TLS" ]

        name?: string
    }
}