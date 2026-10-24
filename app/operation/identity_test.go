package operation

import (
	"context"
	"testing"

	"cczjVideo/app/applog"
)

func TestOwnedContextKeepsOwnerAndLinksOnlySelectedRequestMetadata(t *testing.T) {
	request, stopRequest := context.WithCancel(context.Background())
	request, parent := WithIdentity(request, "rpc.admit")
	request = WithFields(request, applog.Fields{"unselected": "private"})
	owner, stopOwner := context.WithCancel(context.Background())
	defer stopOwner()
	fields := applog.Fields{"source_key": "example", "mode": "novel"}
	ctx, identity := OwnedContext(owner, request, "reading.chapter_text", fields)
	stopRequest()
	if ctx.Err() != nil {
		t.Fatal("owned work inherited short request cancellation")
	}
	if identity.RequestID == parent.RequestID || identity.RequestID == "" {
		t.Fatal("execution did not receive its own identity")
	}
	fields["source_key"] = "mutated"
	actual := Fields(ctx, nil)
	if actual["parent_request_id"] != parent.RequestID || actual["source_key"] != "example" || actual["pipeline"] != "reading.chapter_text" {
		t.Fatal(actual)
	}
	if _, exists := actual["unselected"]; exists {
		t.Fatal("request metadata copied without selection")
	}
	if _, exists := fields["parent_request_id"]; exists {
		t.Fatal("caller fields mutated")
	}
	stopOwner()
	if ctx.Err() != context.Canceled {
		t.Fatal("owner no longer controls execution")
	}
}
