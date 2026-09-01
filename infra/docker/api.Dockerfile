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
USER nobody
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/robot-arena"]

