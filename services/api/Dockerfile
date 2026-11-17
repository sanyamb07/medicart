# Multi-stage build: compile a static binary, then run it in a minimal image.
# Migrations are embedded via go:embed, so the final image needs nothing but
# the binary itself.

FROM golang:1.24-alpine AS build
WORKDIR /src

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && \
    adduser -D -H -u 10001 medicart
WORKDIR /app
COPY --from=build /out/api /app/api
RUN mkdir -p /app/.localstorage && chown -R medicart /app

USER medicart
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=5s --retries=5 CMD ["/app/api", "-healthcheck"]
ENTRYPOINT ["/app/api"]
