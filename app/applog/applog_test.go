package applog

import "testing"

func TestFormatFieldsOrdersKeys(t *testing.T) {
	got := formatFields(Fields{"task_id": "task-1", "source_key": "demo"})
	want := ` fields{source_key="demo",task_id="task-1"}`
	if got != want {
		t.Fatalf("formatFields() = %q, want %q", got, want)
	}
}
