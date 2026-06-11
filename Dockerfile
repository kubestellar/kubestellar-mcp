FROM golang:1.26.4-alpine@sha256:f23e8b227fb4493eabe03bede4d5a32d04092da71962f1fb79b5f7d1e6c2a17f AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /kubestellar-ops ./cmd/kubestellar-ops

FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc

RUN apk add --no-cache ca-certificates \
    && addgroup -g 65532 -S nonroot \
    && adduser -u 65532 -S nonroot -G nonroot

COPY --from=builder /kubestellar-ops /usr/local/bin/kubestellar-ops

# MCP Registry ownership verification label
# See: https://github.com/modelcontextprotocol/registry/blob/main/docs/modelcontextprotocol-io/package-types.mdx
LABEL io.modelcontextprotocol.server.name="io.github.kubestellar/kubestellar-mcp"

USER nonroot:nonroot

ENTRYPOINT ["kubestellar-ops", "--mcp-server"]
