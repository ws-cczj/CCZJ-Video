// Package download owns download-task registration and synchronization.
package download

import "sync"

// Registry is the service-owned index of active and persisted download tasks.
// It only exposes snapshots or explicit lookup/removal operations, so callers
// never need to coordinate access to the task map themselves.
type Registry[T any] struct {
	mu    sync.RWMutex
	tasks map[string]*T
}

// NewRegistry creates an empty task registry.
func NewRegistry[T any]() *Registry[T] {
	return &Registry[T]{tasks: make(map[string]*T)}
}

// Get retrieves a task by ID.
func (r *Registry[T]) Get(id string) (*T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	task, ok := r.tasks[id]
	return task, ok
}

// Add registers task when ID is not already registered.
func (r *Registry[T]) Add(id string, task *T) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tasks[id]; exists {
		return false
	}
	r.tasks[id] = task
	return true
}

// Remove unregisters and returns a task by ID.
func (r *Registry[T]) Remove(id string) (*T, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[id]
	if ok {
		delete(r.tasks, id)
	}
	return task, ok
}

// Snapshot returns a stable copy of registered task pointers.
func (r *Registry[T]) Snapshot() []*T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tasks := make([]*T, 0, len(r.tasks))
	for _, task := range r.tasks {
		tasks = append(tasks, task)
	}
	return tasks
}
