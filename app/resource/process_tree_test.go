package resource

import (
	"errors"
	"reflect"
	"testing"
)

func TestProcessTreeRejectsRecycledParentIDs(t *testing.T) {
	entries := []processTreeEntry{{3, 2, "renderer"}, {2, 1, "webview"}, {1, 0, "app"}, {4, 1, "old-unrelated-service"}, {5, 4, "unrelated-child"}}
	for _, tc := range []struct {
		name       string
		created    map[uint32]uint64
		unreadable uint32
		want       []int
	}{
		{"recycled root PID", map[uint32]uint64{1: 100, 2: 110, 3: 120, 4: 10, 5: 15}, 0, []int{1, 2, 3}},
		{"recycled child PID", map[uint32]uint64{1: 100, 2: 110, 3: 105, 4: 10, 5: 15}, 0, []int{1, 2}},
		{"equal timestamps allowed", map[uint32]uint64{1: 100, 2: 100, 3: 100, 4: 10, 5: 15}, 0, []int{1, 2, 3}},
		{"exited parent excludes subtree", map[uint32]uint64{1: 100, 2: 110, 3: 120, 4: 10, 5: 15}, 2, []int{1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := map[uint32]int{}
			rows, _, _ := collectProcessTree(1, entries, func(pid uint32, name string) (Process, error) {
				reads[pid]++
				if pid == tc.unreadable {
					return Process{}, errors.New("exited")
				}
				return Process{PID: int(pid), Name: name, CreatedAt100NS: tc.created[pid]}, nil
			})
			var got []int
			for _, row := range rows {
				got = append(got, row.PID)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("owned process IDs = %v, want %v; included a process older than its alleged parent", got, tc.want)
			}
			for pid, count := range reads {
				if count > 1 {
					t.Fatalf("PID %d was read %d times", pid, count)
				}
			}
		})
	}
}
