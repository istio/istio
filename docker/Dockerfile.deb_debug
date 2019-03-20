FROM debian:9-slim

# Base image for debug builds - base is 22M
# Built manually uploaded as "istionightly/base_debug"

# Do not add more stuff to this list that isn't small or critically useful.
# If you occasionally need something on the container do
# sudo apt-get update && apt-get whichever
RUN apt-get update && \
    apt-get install --no-install-recommends -y \
      curl \
      iptables \
      iproute2 \
      iputils-ping \
      knot-dnsutils \
      netcat \
      tcpdump \
      net-tools \
      lsof \
      sudo &&  apt-get upgrade -y && \
      apt-get clean -y && \
      rm -rf /var/cache/debconf/* /var/lib/apt/lists/* \
        /var/log/* /tmp/*  /var/tmp/*

# Required:
# iptables (1.8M) (required in init, for debugging in the other cases)
# iproute2 (1.5M) (required for init)

# Debug:
# curl (11M)
# tcpdump (5M)
# netcat (0.2M)
# net-tools (0.7M): netstat
# lsof (0.5M): for debugging open socket, file descriptors
# knot-dnsutils (4.5M): dig/nslookup

# Alternative: dnsutils(44M) for dig/nslookup
