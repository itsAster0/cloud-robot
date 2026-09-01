FROM golang:1.26.7-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /provisioner ./cmd/provisioner

FROM alpine:3.22
RUN apk add --no-cache ca-certificates docker-cli
COPY --from=build /provisioner /usr/local/bin/provisioner
USER root
EXPOSE 8090
ENTRYPOINT ["/usr/local/bin/provisioner"]
