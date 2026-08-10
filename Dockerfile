# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/nexus-api ./cmd/api

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -S nexus && adduser -S -G nexus nexus
COPY --from=build /out/nexus-api /usr/local/bin/nexus-api
USER nexus
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/nexus-api"]
