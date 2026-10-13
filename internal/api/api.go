package api

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/CauaPrates/askMeAnything-Go-React.git/internal/store/pgstore"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Store interface {
	GetRoom(context.Context, uuid.UUID) (pgstore.Room, error)
	GetRooms(context.Context) ([]pgstore.Room, error)
	InsertRoom(context.Context, string) (uuid.UUID, error)
	InsertMessage(context.Context, pgstore.InsertMessageParams) (uuid.UUID, error)
	GetRoomMessages(context.Context, uuid.UUID) ([]pgstore.Message, error)
	GetMessage(context.Context, pgstore.GetMessageParams) (pgstore.Message, error)
	ReactToMessage(context.Context, pgstore.ReactToMessageParams) (int64, error)
	RemoveReactionFromMessage(context.Context, pgstore.RemoveReactionFromMessageParams) (int64, error)
	MarkMessageAsAnswered(context.Context, pgstore.MarkMessageAsAnsweredParams) (pgstore.Message, error)
}

type Handler struct {
	q           Store
	r           *chi.Mux
	upgrader    websocket.Upgrader
	subscribers map[string]map[*subscriber]struct{}
	mu          sync.Mutex
	closed      bool
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.r.ServeHTTP(w, r) }

func NewHandler(q Store) *Handler {
	origins := []string{"http://localhost:5173", "http://127.0.0.1:5173", "http://localhost:4173", "http://127.0.0.1:4173"}
	if configured := strings.TrimSpace(os.Getenv("WSRS_ALLOWED_ORIGINS")); configured != "" {
		origins = nil
		for _, origin := range strings.Split(configured, ",") {
			if origin = strings.TrimSpace(origin); origin != "" {
				origins = append(origins, origin)
			}
		}
	}
	h := &Handler{q: q, subscribers: make(map[string]map[*subscriber]struct{})}
	h.upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		for _, allowed := range origins {
			if origin == allowed {
				return true
			}
		}
		return false
	}}
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{AllowedOrigins: origins, AllowedMethods: []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"}, AllowedHeaders: []string{"Accept", "Content-Type"}, MaxAge: 300}))
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/subscribe/{room_id}", h.handleSubscribe)
	r.Route("/api/rooms", func(r chi.Router) {
		r.Post("/", h.handleCreateRoom)
		r.Get("/", h.handleGetRooms)
		r.Route("/{room_id}", func(r chi.Router) {
			r.Get("/", h.handleGetRoom)
			r.Route("/messages", func(r chi.Router) {
				r.Post("/", h.handleCreateRoomMessage)
				r.Get("/", h.handleGetRoomMessages)
				r.Route("/{message_id}", func(r chi.Router) {
					r.Get("/", h.handleGetRoomMessage)
					r.Patch("/react", h.handleReactToMessage)
					r.Delete("/react", h.handleRemoveReactFromMessage)
					r.Patch("/answer", h.handleMarkMessageAsAnswered)
				})
			})
		})
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Rota não encontrada.")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "Método não permitido.")
	})
	h.r = r
	return h
}

func (h *Handler) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Theme string `json:"theme"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	body.Theme = strings.TrimSpace(body.Theme)
	if !validText(body.Theme, 255) {
		writeError(w, http.StatusBadRequest, "O nome da sala deve ter entre 1 e 255 caracteres.")
		return
	}
	id, err := h.q.InsertRoom(r.Context(), body.Theme)
	if err != nil {
		databaseError(w, err)
		return
	}
	w.Header().Set("Location", "/api/rooms/"+id.String()+"/")
	writeJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
}

func (h *Handler) handleGetRooms(w http.ResponseWriter, r *http.Request) {
	rooms, err := h.q.GetRooms(r.Context())
	if err != nil {
		databaseError(w, err)
		return
	}
	if rooms == nil {
		rooms = []pgstore.Room{}
	}
	writeJSON(w, http.StatusOK, rooms)
}

func (h *Handler) handleGetRoom(w http.ResponseWriter, r *http.Request) {
	if room, ok := h.readRoom(w, r); ok {
		writeJSON(w, http.StatusOK, room)
	}
}

func (h *Handler) handleCreateRoomMessage(w http.ResponseWriter, r *http.Request) {
	room, ok := h.readRoom(w, r)
	if !ok {
		return
	}
	var body struct {
		Message string `json:"message"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	body.Message = strings.TrimSpace(body.Message)
	if !validText(body.Message, 1000) {
		writeError(w, http.StatusBadRequest, "A pergunta deve ter entre 1 e 1000 caracteres.")
		return
	}
	id, err := h.q.InsertMessage(r.Context(), pgstore.InsertMessageParams{RoomID: room.ID, Message: body.Message})
	if err != nil {
		databaseError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
	h.notifyClients(Message{Kind: "message_created", RoomID: room.ID.String(), Value: map[string]any{"id": id.String(), "message": body.Message}})
}

func (h *Handler) handleGetRoomMessages(w http.ResponseWriter, r *http.Request) {
	room, ok := h.readRoom(w, r)
	if !ok {
		return
	}
	messages, err := h.q.GetRoomMessages(r.Context(), room.ID)
	if err != nil {
		databaseError(w, err)
		return
	}
	if messages == nil {
		messages = []pgstore.Message{}
	}
	writeJSON(w, http.StatusOK, messages)
}

func (h *Handler) handleGetRoomMessage(w http.ResponseWriter, r *http.Request) {
	room, ok := h.readRoom(w, r)
	if !ok {
		return
	}
	id, ok := messageID(w, r)
	if !ok {
		return
	}
	message, err := h.q.GetMessage(r.Context(), pgstore.GetMessageParams{ID: id, RoomID: room.ID})
	if err != nil {
		databaseError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, message)
}

func (h *Handler) handleReactToMessage(w http.ResponseWriter, r *http.Request) {
	h.changeReaction(w, r, true)
}
func (h *Handler) handleRemoveReactFromMessage(w http.ResponseWriter, r *http.Request) {
	h.changeReaction(w, r, false)
}

func (h *Handler) changeReaction(w http.ResponseWriter, r *http.Request, increase bool) {
	room, ok := h.readRoom(w, r)
	if !ok {
		return
	}
	id, ok := messageID(w, r)
	if !ok {
		return
	}
	var count int64
	var err error
	kind := "message_reaction_decreased"
	if increase {
		count, err = h.q.ReactToMessage(r.Context(), pgstore.ReactToMessageParams{ID: id, RoomID: room.ID})
		kind = "message_reaction_increased"
	} else {
		count, err = h.q.RemoveReactionFromMessage(r.Context(), pgstore.RemoveReactionFromMessageParams{ID: id, RoomID: room.ID})
	}
	if err != nil {
		databaseError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"count": count})
	h.notifyClients(Message{Kind: kind, RoomID: room.ID.String(), Value: map[string]any{"id": id.String(), "count": count}})
}

func (h *Handler) handleMarkMessageAsAnswered(w http.ResponseWriter, r *http.Request) {
	room, ok := h.readRoom(w, r)
	if !ok {
		return
	}
	id, ok := messageID(w, r)
	if !ok {
		return
	}
	message, err := h.q.MarkMessageAsAnswered(r.Context(), pgstore.MarkMessageAsAnsweredParams{ID: id, RoomID: room.ID})
	if err != nil {
		databaseError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, message)
	h.notifyClients(Message{Kind: "message_answered", RoomID: room.ID.String(), Value: map[string]string{"id": id.String()}})
}
