# The opsschool image for hosted mode: the portal (`opsschool serve`) and
# the session runners it starts. Build it from the repository root:
#
#   docker build -f deploy/Dockerfile -t <registry>/opsschool:<tag> .
#
# See docs/hosted.md.
FROM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd cmd
COPY internal internal
COPY telemetry telemetry
COPY demoapp demoapp
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /opsschool ./cmd/opsschool

FROM ubuntu:24.04
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
ARG TARGETARCH=amd64
ARG KUBECTL_VERSION=v1.35.0
# From https://dl.k8s.io/release/$KUBECTL_VERSION/bin/linux/$TARGETARCH/kubectl.sha256
ARG KUBECTL_SHA256_amd64=a2e984a18a0c063279d692533031c1eff93a262afcc0afdc517375432d060989
ARG KUBECTL_SHA256_arm64=58f82f9fe796c375c5c4b8439850b0f3f4d401a52434052f2df46035a8789e25
RUN apt-get update -q \
 && apt-get install -y -q --no-install-recommends ca-certificates curl \
 && curl -fsSLo /usr/local/bin/kubectl "https://dl.k8s.io/release/$KUBECTL_VERSION/bin/linux/$TARGETARCH/kubectl" \
 && sum="KUBECTL_SHA256_$TARGETARCH" \
 && echo "${!sum}  /usr/local/bin/kubectl" | sha256sum -c - \
 && chmod 0755 /usr/local/bin/kubectl \
 && apt-get purge -y curl && apt-get autoremove -y && apt-get clean && rm -rf /var/lib/apt/lists/*
COPY --from=build /opsschool /usr/local/bin/opsschool
COPY scenarios /usr/share/opsschool/scenarios
ENV OPSSCHOOL_SCENARIOS=/usr/share/opsschool/scenarios \
    OPSSCHOOL_HOME=/var/lib/opsschool
RUN install -d -o 65532 -g 65532 /var/lib/opsschool
USER 65532:65532
ENTRYPOINT []
CMD ["opsschool", "serve"]
