# R1: no secrets or real config are baked into this image.
# All runtime configuration is injected via environment variables.

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/portal ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 10001 -h /app portal \
 && mkdir -p /data \
 && chown portal:portal /data
WORKDIR /app
COPY --from=build /out/portal /app/portal
USER portal
EXPOSE 8080
ENTRYPOINT ["/app/portal"]
