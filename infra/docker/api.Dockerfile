FROM rust:1.98-alpine AS engine
RUN apk add --no-cache musl-dev
WORKDIR /src
COPY crates/arena-engine ./crates/arena-engine
RUN cargo build --release --locked --manifest-path crates/arena-engine/Cargo.toml

FROM golang:1.26.7-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /robot-arena ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /robot-arena /usr/local/bin/robot-arena
COPY --from=engine /src/crates/arena-engine/target/release/arena-engine /usr/local/bin/arena-engine
ENV ARENA_ENGINE_PATH=/usr/local/bin/arena-engine
USER nobody
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/robot-arena"]

