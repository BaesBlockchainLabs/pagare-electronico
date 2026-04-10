package handler

import (
	"encoding/json"
	"net/http"
)

type Handler struct{}

func New() *Handler {
	return &Handler{}
}

func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title":   "Pagaré Electrónico",
		"Message": "Aplicación de pagarés electrónicos con registro en BlockchainFUE",
	}
	WriteJSON(w, http.StatusOK, data)
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"message": "Pagaré Electrónico API - Running",
	})
}

func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
