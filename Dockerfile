# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26.2

FROM golang:${GO_VERSION}-bookworm AS build

WORKDIR /src

ARG BASTION_VERSION=dev

COPY go.work go.work.sum ./
COPY core/go.mod core/go.sum ./core/
RUN cd core && go mod download

COPY core ./core
RUN cd core \
  && go build -trimpath -ldflags "-s -w -X github.com/bastion-computer/bastion/core/internal/config.Version=${BASTION_VERSION}" -o /out/bastion ./cmd/bastion \
  && go build -trimpath -ldflags "-s -w" -o /out/bastion-guest-proxy ./cmd/bastion-guest-proxy

FROM debian:bookworm-slim AS runtime

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates \
  && rm -rf /var/lib/apt/lists/*

ENV BASTION_ADDR=0.0.0.0:3148
ENV BASTION_CLUSTER_ADDR=0.0.0.0:3150

COPY --from=build /out/bastion /usr/local/bin/bastion
COPY --from=build /out/bastion-guest-proxy /usr/local/bin/bastion-guest-proxy

EXPOSE 3148 3150
ENTRYPOINT ["bastion"]
CMD ["version"]
