# Solution for Issue #836

## 🛠️ Proposed Solution (by Aditya Waghamare)

### Analysis
The Claude AI-provider query path (`pkg/ai/claude/client.go`, `sendRequest`) records Prometheus metrics but lacks structured lifecycle logging (`klog`) on success or failure, unlike the MCP tool-call path. This makes incident triage and correlation difficult.

### Fix
Add bounded lifecycle logging using `klog` mirroring `pkg/mcp/server/server.go` conventions. We log provider and duration on success and failure without logging raw error text, request contents, or sensitive API keys.

### Implementation
```go
package claude

import (
	"context"
	"time"

	"k8s.io/klog/v2"
)

func (c *Client) sendRequest(ctx context.Context, prompt string) (string, error) {
	start := time.Now()
	provider := "claude"

	response, err := c.doRequest(ctx, prompt)
	duration := time.Since(start)

	c.metrics.RecordAIQuery(provider, err == nil, duration)

	if err != nil {
		klog.ErrorS(err, "AI query failed", "provider", provider, "duration", duration)
		return "", err
	}

	klog.V(2).InfoS("AI query completed successfully", "provider", provider, "duration", duration)
	return response, nil
}
```

### Testing
Verify via unit tests and check that `klog.ErrorS` and `klog.V(2).InfoS` correctly emit structured logs with `provider` and `duration` fields.

Signed-off-by: Aditya Waghamare <adityawaghamare7620@gmail.com>


---
*Submitted by Aditya Waghamare*
💰 **Payout Address (Base L2 / EVM):** `0xb61dBcdBc3407F71EaCb64D4CBFAcf9FFfe2415C`