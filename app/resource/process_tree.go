package resource

type processTreeEntry struct {
	pid, parent uint32
	name        string
}

// collectProcessTree reads only descendants of this live root, once per sample.
func collectProcessTree(root uint32, entries []processTreeEntry, read func(uint32, string) (Process, error)) ([]Process, int, int) {
	var rootName string
	for _, entry := range entries {
		if entry.pid == root {
			rootName = entry.name
			break
		}
	}
	self, err := read(root, rootName)
	if err != nil {
		return nil, 1, 0
	}
	result := []Process{self}
	owned := map[uint32]Process{root: self}
	examined := map[uint32]bool{root: true}
	skipped, stale := 0, 0
	for changed := true; changed; {
		changed = false
		for _, entry := range entries {
			parent, parentExists := owned[entry.parent]
			if examined[entry.pid] || !parentExists {
				continue
			}
			examined[entry.pid] = true
			child, err := read(entry.pid, entry.name)
			if err != nil {
				skipped++
				continue
			}
			// Windows leaves the numeric parent PID after the parent exits. That
			// PID may later belong to this app or a new WebView process.
			if child.CreatedAt100NS < parent.CreatedAt100NS {
				stale++
				continue
			}
			owned[entry.pid] = child
			result = append(result, child)
			changed = true
		}
	}
	return result, skipped, stale
}
