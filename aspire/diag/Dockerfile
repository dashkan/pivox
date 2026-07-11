# syntax=docker/dockerfile:1
# Network diagnostics sidecar: latest Ubuntu LTS + a kit of network/service
# probing tools. The apphost gives this container a WithReference to every
# other resource, so their connection strings + service-discovery endpoints
# land in its env. Exec in and probe:
#   docker exec -it <diag-container> bash
#   env | sort | grep -Ei 'ConnectionStrings|services__'   # what's reachable
# Tools:
#   ping / ip / ss / traceroute / mtr / tcpdump   — L3/L4 reachability
#   dig / nslookup / host                          — container-network DNS
#   nc / socat / curl / wget                       — TCP + HTTP probes
#   psql / kcat / jq                               — postgres + kafka probes
# Stays up via `sleep infinity` so it's always exec-able while the stack runs.
FROM ubuntu:latest
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
      iputils-ping \
      iproute2 \
      dnsutils \
      netcat-openbsd \
      traceroute \
      mtr-tiny \
      tcpdump \
      net-tools \
      socat \
      curl \
      wget \
      ca-certificates \
      postgresql-client \
      kcat \
      jq \
    && rm -rf /var/lib/apt/lists/*
CMD ["sleep", "infinity"]
