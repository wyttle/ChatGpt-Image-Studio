package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

type imageTaskRunner func(ctx context.Context, r *http.Request) (map[string]any, error)

func wantsAsyncImageTask(r *http.Request) bool {
	if r == nil {
		return false
	}
	value := strings.TrimSpace(strings.ToLower(r.Header.Get("X-Image-Task")))
	return value == "1" || value == "true" || value == "async"
}

func (s *Server) startImageTask(w http.ResponseWriter, r *http.Request, run imageTaskRunner) {
	if s == nil || s.imageTasks == nil || run == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "image task runner is unavailable"})
		return
	}
	id := newImageTaskID()
	s.imageTasks.create(id)
	writeJSON(w, http.StatusAccepted, map[string]any{"task_id": id, "status": string(imageTaskPending)})

	go func() {
		taskReq := r.Clone(context.Background())
		ctx := context.Background()
		s.imageTasks.update(id, func(task *imageTask) {
			task.Status = imageTaskRunning
		})
		payload, err := run(ctx, taskReq)
		if err != nil {
			s.imageTasks.update(id, func(task *imageTask) {
				task.Status = imageTaskError
				task.Error = err.Error()
			})
			return
		}
		s.imageTasks.update(id, func(task *imageTask) {
			task.Status = imageTaskSuccess
			task.Result = payload
		})
	}()
}

func (s *Server) handleGetImageTask(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "task id is required"})
		return
	}
	task, ok := s.imageTasks.get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "task not found"})
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func newImageTaskID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return hex.EncodeToString([]byte(strings.ReplaceAll(strings.ToLower(http.StatusText(http.StatusAccepted)), " ", "")))
	}
	return hex.EncodeToString(data[:])
}
