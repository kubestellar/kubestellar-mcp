FROM golang:1.27.1-alpine@sha256:4cb7ac979db5fcc41cae44b2227ba5ab8a51e8807f40d9ba4dee20a0ad960b5b AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /kubestellar-ops ./cmd/kubestellar-ops

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

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
