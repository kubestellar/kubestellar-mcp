package upgrades

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CheckHelmReleaseUpgrades checks Helm releases in the cluster.
func CheckHelmReleaseUpgrades(ctx context.Context, ca ClusterAccess, args map[string]interface{}) (string, bool) {
	cluster, _ := args["cluster"].(string)
	namespace, _ := args["namespace"].(string)

	client, err := ca.GetClientForCluster(cluster)
	if err != nil {
		return fmt.Sprintf("Failed to create client: %v", err), true
	}

	var sb strings.Builder
	sb.WriteString("# Helm Releases\n\n")

	labelSelector := "owner=helm"
	var secrets *corev1.SecretList
	if namespace == "" {
		secrets, err = client.CoreV1().Secrets("").List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
	} else {
		secrets, err = client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
	}

	if err != nil {
		return fmt.Sprintf("Failed to list Helm secrets: %v", err), true
	}

	if len(secrets.Items) == 0 {
		sb.WriteString("**Helm Releases Found:** 0\n\n")
		sb.WriteString("No Helm releases found in the cluster.\n")
		return sb.String(), false
	}

	releases := make(map[string]HelmRelease)
	for _, secret := range secrets.Items {
		release := ParseHelmSecret(&secret)
		if release == nil {
			continue
		}

		key := release.Namespace + "/" + release.Name
		existing, found := releases[key]
		if !found || release.Revision > existing.Revision {
			releases[key] = *release
		}
	}

	_, _ = fmt.Fprintf(&sb, "**Helm Releases Found:** %d\n\n", len(releases))

	sb.WriteString("| Release | Namespace | Chart | Version | App Version | Status |\n")
	sb.WriteString("|---------|-----------|-------|---------|-------------|--------|\n")

	for _, rel := range releases {
		_, _ = fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s | %s |\n",
			rel.Name, rel.Namespace, rel.Chart, rel.Version, rel.AppVer, rel.Status)
	}

	sb.WriteString("\n## Checking for Updates\n\n")
	sb.WriteString("To check for available chart updates, you need to:\n\n")
	sb.WriteString("1. Ensure Helm repos are added: `helm repo list`\n")
	sb.WriteString("2. Update repos: `helm repo update`\n")
	sb.WriteString("3. Search for updates: `helm search repo <chart-name>`\n\n")
	sb.WriteString("**Note:** This tool shows currently deployed releases. Checking for newer chart versions\n")
	sb.WriteString("requires access to Helm repositories which are typically configured on the client side.\n")

	return sb.String(), false
}

// ParseHelmSecret decodes a Helm release from a Kubernetes secret.
func ParseHelmSecret(secret *corev1.Secret) *HelmRelease {
	if secret.Type != "helm.sh/release.v1" {
		return nil
	}

	releaseData, ok := secret.Data["release"]
	if !ok {
		return nil
	}

	decoded, err := base64.StdEncoding.DecodeString(string(releaseData))
	if err != nil {
		decoded = releaseData
	}

	reader, err := gzip.NewReader(bytes.NewReader(decoded))
	if err != nil {
		return nil
	}
	defer func() {
		_ = reader.Close()
	}()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return nil
	}

	var releaseObj map[string]interface{}
	if err := json.Unmarshal(decompressed, &releaseObj); err != nil {
		return nil
	}

	release := &HelmRelease{
		Namespace: secret.Namespace,
	}

	if name, ok := releaseObj["name"].(string); ok {
		release.Name = name
	}

	if info, ok := releaseObj["info"].(map[string]interface{}); ok {
		if status, ok := info["status"].(string); ok {
			release.Status = status
		}
	}

	if chart, ok := releaseObj["chart"].(map[string]interface{}); ok {
		if metadata, ok := chart["metadata"].(map[string]interface{}); ok {
			if name, ok := metadata["name"].(string); ok {
				release.Chart = name
			}
			if ver, ok := metadata["version"].(string); ok {
				release.Version = ver
			}
			if appVer, ok := metadata["appVersion"].(string); ok {
				release.AppVer = appVer
			}
		}
	}

	if version, ok := releaseObj["version"].(float64); ok {
		release.Revision = int(version)
	}

	return release
}
