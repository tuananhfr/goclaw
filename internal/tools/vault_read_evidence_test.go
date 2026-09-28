package tools

import (
	"context"
	"testing"
)

func TestVaultEvidenceRequiresActualReturnedContent(t *testing.T) {
	ctx, evidence := WithVaultReadEvidence(context.Background())
	if evidence.Contains("quote") {
		t.Fatal("empty evidence accepted")
	}
	recordVaultRead(ctx, "doc", "An approved quote from the document.")
	if !evidence.Contains("approved quote") || evidence.Contains("invented quote") || evidence.Contains("") {
		t.Fatal("evidence did not match exact content")
	}
	_, other := WithVaultReadEvidence(context.Background())
	if other.Contains("approved quote") {
		t.Fatal("evidence leaked between runs")
	}
}
