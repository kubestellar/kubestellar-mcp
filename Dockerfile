FROM golang:1.24.1-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /kubestellar-ops ./cmd/kubestellar-ops

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=builder /kubestellar-ops /usr/local/bin/kubestellar-ops

ENTRYPOINT ["kubestellar-ops", "--mcp-server"]
