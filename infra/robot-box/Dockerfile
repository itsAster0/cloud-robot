FROM golang:1.26.7-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY internal ./internal
COPY cmd/box-supervisor ./cmd/box-supervisor
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /robot-box-supervisor ./cmd/box-supervisor

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates openssh-server lua5.4 lua-http lua-dkjson \
    && useradd --create-home --shell /bin/bash developer \
    && usermod -p '*' developer \
    && mkdir -p /run/sshd /workspace /opt/robot-arena-sdk \
    && chown developer:developer /workspace \
    && rm -rf /var/lib/apt/lists/*

COPY sdk/lua/arena.lua /opt/robot-arena-sdk/arena.lua
COPY infra/robot-box/sshd_config /etc/ssh/sshd_config
COPY infra/robot-box/entrypoint.sh /usr/local/bin/robot-box-entrypoint
COPY --from=build /robot-box-supervisor /usr/local/bin/robot-box-supervisor

ENV LUA_PATH="/workspace/?.lua;/opt/robot-arena-sdk/?.lua;/usr/share/lua/5.1/?.lua;/usr/share/lua/5.1/?/init.lua;/usr/share/lua/5.2/?.lua;/usr/share/lua/5.2/?/init.lua;/usr/share/lua/5.3/?.lua;/usr/share/lua/5.3/?/init.lua;;"
WORKDIR /workspace
EXPOSE 22
ENTRYPOINT ["/usr/local/bin/robot-box-entrypoint"]
