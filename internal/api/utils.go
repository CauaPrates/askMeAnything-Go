package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/CauaPrates/askMeAnything-Go-React.git/internal/store/pgstore"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "serialization error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func decodeBody(w http.ResponseWriter, r *http.Request, value any) bool {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Envie o corpo como application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "JSON inválido ou corpo muito grande.")
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Envie apenas um objeto JSON.")
		return false
	}
	return true
}

func validText(value string, limit int) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= limit
}

func databaseError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Sala ou pergunta não encontrada.")
		return
	}
	slog.Error("database operation failed", "error", err)
	writeError(w, http.StatusInternalServerError, "Não foi possível concluir a operação.")
}

func (h *Handler) readRoom(w http.ResponseWriter, r *http.Request) (pgstore.Room, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "room_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "Identificador de sala inválido.")
		return pgstore.Room{}, false
	}
	room, err := h.q.GetRoom(r.Context(), id)
	if err != nil {
		databaseError(w, err)
		return pgstore.Room{}, false
	}
	return room, true
}

func messageID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "message_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "Identificador de pergunta inválido.")
		return uuid.Nil, false
	}
	return id, true
}
