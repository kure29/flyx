package handler

import (
	"net/http"

	"go-backend/internal/http/response"
)

func (h *Handler) storageSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	if h == nil || h.repo == nil {
		response.WriteJSON(w, response.Err(-2, "repository not initialized"))
		return
	}

	summary, err := h.repo.DatabaseStorageSummary()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(summary))
}
