FROM golang:1.25.14-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X github.com/btraven00/hapiq/internal/version.Version=${VERSION}" \
      -o /hapiq .

FROM scratch
COPY --from=builder /hapiq /hapiq
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ENTRYPOINT ["/hapiq"]
