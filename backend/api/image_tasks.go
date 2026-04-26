package api

import (
	"sync"
	"time"
)

type imageTaskStatus string

const (
	imageTaskPending imageTaskStatus = "pending"
	imageTaskRunning imageTaskStatus = "running"
	imageTaskSuccess imageTaskStatus = "success"
	imageTaskError   imageTaskStatus = "error"
)

type imageTask struct {
	ID        string          `json:"id"`
	Status    imageTaskStatus `json:"status"`
	CreatedAt int64           `json:"created_at"`
	UpdatedAt int64           `json:"updated_at"`
	Result    map[string]any  `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type imageTaskStore struct {
	mu    sync.RWMutex
	items map[string]*imageTask
}

func newImageTaskStore() *imageTaskStore {
	return &imageTaskStore{items: make(map[string]*imageTask)}
}

func (s *imageTaskStore) create(id string) *imageTask {
	now := time.Now().Unix()
	task := &imageTask{ID: id, Status: imageTaskPending, CreatedAt: now, UpdatedAt: now}
	s.mu.Lock()
	s.items[id] = task
	s.pruneLocked(now)
	s.mu.Unlock()
	return cloneImageTask(task)
}

func (s *imageTaskStore) get(id string) (*imageTask, bool) {
	s.mu.RLock()
	task, ok := s.items[id]
	cloned := cloneImageTask(task)
	s.mu.RUnlock()
	return cloned, ok
}

func (s *imageTaskStore) update(id string, fn func(task *imageTask)) {
	s.mu.Lock()
	if task, ok := s.items[id]; ok {
		fn(task)
		task.UpdatedAt = time.Now().Unix()
	}
	s.mu.Unlock()
}

func (s *imageTaskStore) pruneLocked(now int64) {
	const ttl = int64(6 * 60 * 60)
	for id, task := range s.items {
		if now-task.UpdatedAt > ttl {
			delete(s.items, id)
		}
	}
}

func cloneImageTask(task *imageTask) *imageTask {
	if task == nil {
		return nil
	}
	cloned := *task
	return &cloned
}
