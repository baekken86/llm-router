package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/chris/llm-router/internal/service"
)

type AdminHandler struct {
	adminService service.AdminService
}

func NewAdminHandler(adminService service.AdminService) *AdminHandler {
	return &AdminHandler{adminService: adminService}
}

func (h *AdminHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/auth", h.Auth)
	return r
}

func (h *AdminHandler) Auth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if !h.adminService.ValidatePassword(req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}

	token := h.adminService.CreateSession()

	writeJSON(w, http.StatusOK, map[string]string{
		"token": token,
	})
}
