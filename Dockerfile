FROM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /kubestellar-ops ./cmd/kubestellar-ops

FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a

RUN apk add --no-cache ca-certificates \
    && addgroup -g 65532 -S nonroot \
    && adduser -u 65532 -S nonroot -G nonroot

COPY --from=builder /kubestellar-ops /usr/local/bin/kubestellar-ops

# MCP Registry ownership verification label
# See: https://github.com/modelcontextprotocol/registry/blob/main/docs/modelcontextprotocol-io/package-types.mdx
LABEL io.modelcontextprotocol.server.name="io.github.kubestellar/kubestellar-mcp"

USER nonroot:nonroot

# The MCP server uses stdio transport (not HTTP), so a process-existence check
# is the appropriate liveness signal. This catches OOM kills, panics, and hangs.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD pgrep -x kubestellar-ops > /dev/null || exit 1

ENTRYPOINT ["kubestellar-ops", "--mcp-server"]
