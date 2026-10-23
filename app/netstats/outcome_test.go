package netstats

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestCancellationDoesNotInflateFailureRate(t *testing.T) {
	reset()
	defer reset()
	Record(CategoryPlayback, 12, 5, fmt.Errorf("body: %w", context.Canceled))
	Record(CategoryPlayback, 10, 8, context.DeadlineExceeded)
	Record(CategoryPlayback, 0, 2, errors.New("connection lost"))
	Record(CategoryPlayback, 20, 1, nil)
	rows := Snapshot()
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	row := rows[0]
	if row.Requests != 4 || row.Fails != 2 || row.Cancelled != 1 || row.Timeouts != 1 || row.Bytes != 42 {
		t.Fatalf("outcomes lost or cancellation treated as a failure: %+v", row)
	}
	Record(CategoryPlayback, 0, 1, context.Canceled)
	if Snapshot()[0].LastError != "connection lost" {
		t.Fatal("cancellation overwrote the actual last failure")
	}
}
