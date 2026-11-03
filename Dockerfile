FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /valto-notification ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && addgroup -g 10001 app && adduser -D -u 10001 -G app app
WORKDIR /app
COPY --from=build /valto-notification /app/valto-notification
USER app
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD wget -q -O /dev/null http://127.0.0.1:${PORT:-8080}/healthz || exit 1
ENTRYPOINT ["/app/valto-notification"]
