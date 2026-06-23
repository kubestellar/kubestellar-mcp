package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
)

var (
	// helmBlockedCGNATNet is RFC 6598 Carrier-Grade NAT space (100.64.0.0/10).
	// Not covered by net.IP.IsPrivate() but often routes to internal services.
	_, helmBlockedCGNATNet, _ = net.ParseCIDR("100.64.0.0/10")

	// helmBlockedCloudMetaNet is the cloud instance metadata service (169.254.169.254/32).
	// This is the primary SSRF target for credential theft in AWS, GCP, and Azure.
	_, helmBlockedCloudMetaNet, _ = net.ParseCIDR("169.254.169.254/32")
)