package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CauaPrates/askMeAnything-Go-React.git/internal/store/pgstore"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCreateRoomRejectsInvalidInput(t *testing.T) {
	for _, test := range []struct {
		name, body, contentType string
		status                  int
	}{
		{"empty", `{"theme":""}`, "application/json", 400},
		{"whitespace", `{"theme":"   "}`, "application/json", 400},
		{"too long", `{"theme":"` + strings.Repeat("a", 256) + `"}`, "application/json", 400},
		{"invalid json", `{`, "application/json", 400},
		{"unknown field", `{"theme":"Go","admin":true}`, "application/json", 400},
		{"multiple objects", `{"theme":"Go"}{"theme":"Other"}`, "application/json", 400},
		{"wrong type", `{"theme":123}`, "application/json", 400},
		{"missing content type", `{"theme":"Go"}`, "", 415},
		{"too large", `{"theme":"` + strings.Repeat("a", 9000) + `"}`, "application/json", 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := NewHandler(nil)
			defer handler.Close()
			req := httptest.NewRequest(http.MethodPost, "/api/rooms/", strings.NewReader(test.body))
			req.Header.Set("Content-Type", test.contentType)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != test.status {
				t.Fatalf("status=%d, want=%d: %s", res.Code, test.status, res.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body["error"] == "" {
				t.Fatalf("expected JSON error: %s", res.Body.String())
			}
		})
	}
}

func TestHealthAndCors(t *testing.T) {
	t.Setenv("WSRS_ALLOWED_ORIGINS", "http://localhost:5173")
	handler := NewHandler(nil)
	defer handler.Close()
	for _, origin := range []string{"http://localhost:5173", "https://untrusted.example"} {
		req := httptest.NewRequest("GET", "/health", nil)
		req.Header.Set("Origin", origin)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatal(res.Code)
		}
		allowed := res.Header().Get("Access-Control-Allow-Origin")
		if origin == "http://localhost:5173" && allowed != origin {
			t.Fatalf("missing allowed origin: %s", allowed)
		}
		if origin != "http://localhost:5173" && allowed != "" {
			t.Fatalf("untrusted origin allowed: %s", allowed)
		}
	}
}

func integrationServer(t *testing.T) (*Handler, *httptest.Server, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("AMA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set AMA_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+quotedSchema+" CASCADE")
		_ = admin.Close(ctx)
	})
	migrations, err := filepath.Glob("../store/pgstore/migrations/*.sql")
	if err != nil || len(migrations) != 3 {
		t.Fatalf("migrations: %v %v", migrations, err)
	}
	for _, file := range migrations {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		up := strings.Split(string(data), "---- create above / drop below ----")[0]
		if _, err := pool.Exec(ctx, up); err != nil {
			t.Fatalf("migration %s: %v", file, err)
		}
	}
	handler := NewHandler(pgstore.New(pool))
	server := httptest.NewServer(handler)
	t.Cleanup(func() { handler.Close(); server.Close() })
	return handler, server, pool
}

func httpRequest(t *testing.T, server *httptest.Server, method, path, body string, status int) []byte {
	t.Helper()
	req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(res.Body); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != status {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, res.StatusCode, status, buf.String())
	}
	return buf.Bytes()
}

func createdID(t *testing.T, body []byte) string {
	t.Helper()
	var data struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(data.ID); err != nil {
		t.Fatal(err)
	}
	return data.ID
}

func TestIntegrationRoomAndQuestionLifecycle(t *testing.T) {
	_, server, pool := integrationServer(t)
	room := createdID(t, httpRequest(t, server, "POST", "/api/rooms/", `{"theme":"  Go community  "}`, 201))
	other := createdID(t, httpRequest(t, server, "POST", "/api/rooms/", `{"theme":"Other room"}`, 201))
	base := "/api/rooms/" + room + "/"
	var roomData map[string]any
	_ = json.Unmarshal(httpRequest(t, server, "GET", base, "", 200), &roomData)
	if roomData["id"] != room || roomData["theme"] != "Go community" {
		t.Fatal(roomData)
	}
	if string(httpRequest(t, server, "GET", base+"messages/", "", 200)) != "[]" {
		t.Fatal("empty list must be []")
	}
	question := createdID(t, httpRequest(t, server, "POST", base+"messages/", `{"message":"How do goroutines work with cancellation?"}`, 201))
	item := base + "messages/" + question + "/"
	for _, bad := range []string{"", "  ", strings.Repeat("a", 1001)} {
		body, _ := json.Marshal(map[string]string{"message": bad})
		httpRequest(t, server, "POST", base+"messages/", string(body), 400)
	}
	httpRequest(t, server, "GET", "/api/rooms/not-a-uuid/", "", 400)
	httpRequest(t, server, "GET", "/api/rooms/"+uuid.NewString()+"/", "", 404)
	httpRequest(t, server, "GET", base+"messages/"+uuid.NewString()+"/", "", 404)
	for _, action := range []struct{ method, suffix string }{{"GET", ""}, {"PATCH", "react"}, {"DELETE", "react"}, {"PATCH", "answer"}} {
		httpRequest(t, server, action.method, "/api/rooms/"+other+"/messages/"+question+"/"+action.suffix, "", 404)
	}
	httpRequest(t, server, "GET", item, "", 200)
	for _, step := range []struct {
		method string
		count  int
	}{{"DELETE", 0}, {"PATCH", 1}, {"PATCH", 2}, {"DELETE", 1}, {"DELETE", 0}, {"DELETE", 0}} {
		var data struct {
			Count int `json:"count"`
		}
		_ = json.Unmarshal(httpRequest(t, server, step.method, item+"react", "", 200), &data)
		if data.Count != step.count {
			t.Fatalf("count=%d want=%d", data.Count, step.count)
		}
	}
	var result pgstore.Message
	_ = json.Unmarshal(httpRequest(t, server, "PATCH", item+"answer", "", 200), &result)
	if !result.Answered {
		t.Fatal("question not answered")
	}
	var questions []pgstore.Message
	_ = json.Unmarshal(httpRequest(t, server, "GET", base+"messages/", "", 200), &questions)
	if len(questions) != 1 || !questions[0].Answered {
		t.Fatal(questions)
	}
	if _, err := pool.Exec(context.Background(), "UPDATE messages SET reaction_count = -1 WHERE id = $1", question); err == nil {
		t.Fatal("database accepted negative reactions")
	}
}

func TestIntegrationConcurrentReactions(t *testing.T) {
	_, server, _ := integrationServer(t)
	room := createdID(t, httpRequest(t, server, "POST", "/api/rooms/", `{"theme":"Concurrency"}`, 201))
	base := "/api/rooms/" + room + "/messages/"
	question := createdID(t, httpRequest(t, server, "POST", base, `{"message":"Concurrent reactions"}`, 201))
	for _, method := range []string{"PATCH", "DELETE"} {
		var wait sync.WaitGroup
		failures := make(chan error, 20)
		for i := 0; i < 20; i++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				req, err := http.NewRequest(method, server.URL+base+question+"/react", nil)
				if err != nil {
					failures <- err
					return
				}
				res, err := server.Client().Do(req)
				if err != nil {
					failures <- err
					return
				}
				defer res.Body.Close()
				if res.StatusCode != 200 {
					failures <- fmt.Errorf("reaction returned %d", res.StatusCode)
				}
			}()
		}
		wait.Wait()
		close(failures)
		for err := range failures {
			t.Error(err)
		}
		var result pgstore.Message
		_ = json.Unmarshal(httpRequest(t, server, "GET", base+question+"/", "", 200), &result)
		want := int64(20)
		if method == "DELETE" {
			want = 0
		}
		if result.ReactionCount != want {
			t.Fatalf("count=%d want=%d", result.ReactionCount, want)
		}
	}
}

func TestIntegrationWebSocketEventsAndCleanup(t *testing.T) {
	handler, server, _ := integrationServer(t)
	room := createdID(t, httpRequest(t, server, "POST", "/api/rooms/", `{"theme":"Live room"}`, 201))
	other := createdID(t, httpRequest(t, server, "POST", "/api/rooms/", `{"theme":"Isolated room"}`, 201))
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/subscribe/" + room
	blocked, res, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": []string{"https://untrusted.example"}})
	if blocked != nil {
		_ = blocked.Close()
	}
	if err == nil || res == nil || res.StatusCode != 403 {
		t.Fatal("untrusted websocket origin accepted")
	}
	if res != nil {
		_ = res.Body.Close()
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": []string{"http://localhost:5173"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	base := "/api/rooms/" + room + "/messages/"
	httpRequest(t, server, "POST", "/api/rooms/"+other+"/messages/", `{"message":"Unrelated event"}`, 201)
	question := createdID(t, httpRequest(t, server, "POST", base, `{"message":"First live question"}`, 201))
	expect := func(kind string) {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var event struct {
			Kind  string `json:"kind"`
			Value struct {
				ID string `json:"id"`
			} `json:"value"`
		}
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event.Kind != kind || event.Value.ID != question {
			t.Fatalf("unexpected event: %+v", event)
		}
	}
	expect("message_created")
	for _, step := range []struct{ method, action, kind string }{
		{"PATCH", "react", "message_reaction_increased"}, {"DELETE", "react", "message_reaction_decreased"}, {"PATCH", "answer", "message_answered"},
	} {
		httpRequest(t, server, step.method, base+question+"/"+step.action, "", 200)
		expect(step.kind)
	}
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	_ = conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		handler.mu.Lock()
		count := len(handler.subscribers)
		handler.mu.Unlock()
		if count == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("subscriber leaked after disconnect in room %s", room))
}
