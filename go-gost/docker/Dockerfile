# Node agent image. The release job builds it from the same UPX-compressed
# binaries it publishes (gost-amd64 / gost-arm64 next to this file); run
# with --network host so forwards can listen on any port.
FROM debian:trixie-slim

# nftables: traffic counters and inbound whitelists; dsniff: tcpkill.
# bookworm's nftables 1.0.6 crashed on newer host kernels; trixie ships 1.1.x.
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates tzdata nftables dsniff tini bash iperf3 iproute2 coreutils procps gawk \
  && rm -rf /var/lib/apt/lists/*

ARG TARGETARCH
ARG VERSION=dev
# Artifacts lose the exec bit on upload.
COPY --chmod=755 gost-${TARGETARCH} /usr/local/bin/flyx-agent
COPY --chmod=755 entrypoint.sh /usr/local/bin/flyx-agent-entrypoint

ENV FLYX_AGENT_CONTAINER=1 \
    FLUX_AGENT_CONTAINER=1 \
    FLYX_AGENT_IMAGE_VERSION=${VERSION}

VOLUME /etc/flyx-agent
WORKDIR /etc/flyx-agent
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/flyx-agent-entrypoint"]
