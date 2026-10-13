# AMA — Ask Me Anything API

**English** | [Português (Brasil)](README.pt-BR.md)

A **Go** backend for questions and answers in topic-based rooms, with **PostgreSQL** persistence and real-time notifications over **WebSocket**.

The API supports room creation, question submission, reaction counts, and marking questions as answered. Changes are broadcast to clients connected to the corresponding room.

## Features

- Create, list, and retrieve rooms by ID.
- Submit, list, and retrieve questions within a room.
- Increment and decrement a question's reaction count.
- Mark questions as answered.
- Subscribe to room events over WebSocket.
- Persist data with UUID identifiers and relationships between rooms and questions.

## Tech stack

| Technology | Purpose |
| --- | --- |
| Go | HTTP server and API logic |
| Chi | Routing, request identification, CORS, and panic recovery |
| PostgreSQL | Room and question persistence |
| pgx | Database access and connection pooling |
| sqlc | Generating typed Go code from SQL queries |
| Gorilla WebSocket | Real-time connections and event delivery |
| Tern | Database migrations |
| godotenv | Loading configuration from `.env` |
| Docker | Local database environment |

## Running locally

Prerequisites: Git, **Go 1.22.5 or later**, Docker running, and [Tern](https://github.com/jackc/tern) available on your `PATH`.

### 1. Clone the repository

```bash
git clone https://github.com/CauaPrates/askMeAnything-Go.git
cd askMeAnything-Go
go mod download
```

### 2. Configure the environment

Use `.env.example` as a reference to create or configure `.env` at the repository root with your PostgreSQL settings. Example for local development:

```dotenv
WSRS_DATABASE_HOST=localhost
WSRS_DATABASE_PORT=5432
WSRS_DATABASE_NAME=ama
WSRS_DATABASE_USER=ama
WSRS_DATABASE_PASSWORD=ama_local
WSRS_DATABASE_SSLMODE=disable
WSRS_HTTP_ADDRESS=:8080
WSRS_ALLOWED_ORIGINS=http://localhost:5173,http://127.0.0.1:5173,http://localhost:4173,http://127.0.0.1:4173
```

| Variable | Purpose |
| --- | --- |
| `WSRS_DATABASE_HOST` | PostgreSQL host |
| `WSRS_DATABASE_PORT` | Database connection port |
| `WSRS_DATABASE_NAME` | Database name |
| `WSRS_DATABASE_USER` | Database user |
| `WSRS_DATABASE_PASSWORD` | Database password |
| `WSRS_DATABASE_SSLMODE` | PostgreSQL SSL mode; `disable` in the local example, `prefer` when unset |
| `WSRS_HTTP_ADDRESS` | Server address; defaults to `:8080` |
| `WSRS_ALLOWED_ORIGINS` | Comma-separated list of allowed frontend origins |

These values are local examples. The server and migration tool load `.env` from their working directory.

### 3. Start PostgreSQL

With the values configured in `.env`:

```bash
docker compose up -d --wait db
```

Compose uses PostgreSQL 16, persistent storage, and a health check. The volume follows the [layout for this version of the official image](https://hub.docker.com/_/postgres). To also start pgAdmin:

```bash
docker compose --profile tools up -d --wait
```

pgAdmin is available at `http://localhost:8082`. Its service settings are defined in `compose.yml`.

### 4. Apply migrations

You can install a specific version of Tern with:

```bash
go install github.com/jackc/tern/v2@v2.2.0
```

With the `tern` executable on your `PATH`, run the tool included in the project:

```bash
go run ./cmd/tools/terndotenv
```

It loads the environment variables from `.env` and applies the migrations in `internal/store/pgstore/migrations/` using `tern.conf`.

### 5. Start the API

```bash
go run ./cmd/wsrs
```

The API is available at **http://localhost:8080**. To list rooms:

```bash
curl http://localhost:8080/api/rooms/
```

## Endpoints

HTTP base URL: `http://localhost:8080`.

| Method | Route | Operation |
| --- | --- | --- |
| `GET` | `/health` | Check whether the HTTP process is running |
| `POST` | `/api/rooms/` | Create a room |
| `GET` | `/api/rooms/` | List rooms |
| `GET` | `/api/rooms/{room_id}/` | Retrieve a room |
| `POST` | `/api/rooms/{room_id}/messages/` | Submit a question |
| `GET` | `/api/rooms/{room_id}/messages/` | List room questions |
| `GET` | `/api/rooms/{room_id}/messages/{message_id}/` | Retrieve a question |
| `PATCH` | `/api/rooms/{room_id}/messages/{message_id}/react` | Add a reaction |
| `DELETE` | `/api/rooms/{room_id}/messages/{message_id}/react` | Remove a reaction |
| `PATCH` | `/api/rooms/{room_id}/messages/{message_id}/answer` | Mark a question as answered |
| `GET` with WebSocket upgrade | `/subscribe/{room_id}` | Subscribe to room events |

Room and question IDs are UUIDs. Questions are called `messages` in the API contract.

### Create a room

Send a `POST /api/rooms/` request with `Content-Type: application/json`:

```json
{
  "theme": "Questions about Go"
}
```

The response uses HTTP `201 Created` and contains the new ID:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000"
}
```

### Submit a question

Send a `POST /api/rooms/{room_id}/messages/` request, replacing `{room_id}` with the ID returned when creating the room:

```json
{
  "message": "How do I use Go?"
}
```

The response uses HTTP `201 Created` and also contains an `id` field. The schema allows up to 255 characters for a room's theme and up to 1,000 characters for a question. Leading and trailing whitespace is removed before saving; empty input is rejected.

Adding or removing a reaction returns the updated count in the `count` field.

Message queries use both the room ID and question ID. Question lists prioritize unanswered questions and sort by reaction count. A database constraint keeps the counter nonnegative.

Error responses use a JSON object with an `error` field. Malformed IDs return `400`, missing resources return `404`, and request bodies without `Content-Type: application/json` return `415`.

## Real-time events

Connect a WebSocket client to:

```text
ws://localhost:8080/subscribe/{room_id}
```

| Event | Contents of `value` |
| --- | --- |
| `message_created` | The question's `id` and `message` |
| `message_reaction_increased` | `id` and the updated `count` |
| `message_reaction_decreased` | `id` and the updated `count` |
| `message_answered` | The answered question's `id` |

Example notification:

```json
{
  "kind": "message_created",
  "value": {
    "id": "550e8400-e29b-41d4-a716-446655440001",
    "message": "How do I use Go?"
  }
}
```

HTTP operations modify the data; WebSocket delivers events to room subscribers. Subscriptions are held in memory by the API process.

## Project structure

```text
askMeAnything-Go/
├── cmd/
│   ├── wsrs/
│   │   └── main.go              # Server entry point
│   └── tools/
│       └── terndotenv/
│           └── main.go          # Running migrations with .env
├── internal/
│   ├── api/
│   │   ├── api.go               # Routes and HTTP handlers
│   │   ├── utils.go             # Validation and JSON responses
│   │   ├── websocket.go         # Subscriptions and connection lifecycle
│   │   └── api_test.go          # HTTP and integration tests
│   └── store/
│       └── pgstore/
│           ├── migrations/      # Schema and Tern configuration
│           ├── queries/         # SQL queries
│           ├── sqlc.yaml        # Code generation configuration
│           ├── db.go
│           ├── models.go
│           └── queries.sql.go
├── docs/
│   └── Rooms.postman_collection.json
├── README.md
├── README.pt-BR.md
├── compose.yml
├── gen.go
├── go.mod
└── go.sum
```

The server initializes the connection pool and injects database queries into the HTTP handlers. The `pgstore` layer uses code generated by sqlc from the SQL queries and schema.

The `compose.yml` file defines the local PostgreSQL environment and the optional pgAdmin service.

## Development tools

| Command | Purpose |
| --- | --- |
| `go run ./cmd/wsrs` | Run the API |
| `go run ./cmd/tools/terndotenv` | Apply migrations |
| `go build ./...` | Compile packages |
| `go vet ./...` | Run static analysis |
| `go test ./...` | Run tests; integration tests are enabled by `AMA_TEST_DATABASE_URL` |
| `sqlc generate -f ./internal/store/pgstore/sqlc.yaml` | Regenerate query code |

To regenerate queries, install [sqlc](https://docs.sqlc.dev/) and run the command from the repository root. The generated files record version `1.27.0`.

The [Rooms Postman collection](docs/Rooms.postman_collection.json) includes examples of HTTP operations. Import it and fill in the room and question parameters using IDs returned by the API.

## PostgreSQL integration tests

Run HTTP validation tests with `go test ./...`. To enable persistence and WebSocket tests, set `AMA_TEST_DATABASE_URL` to a test PostgreSQL connection. Example in PowerShell:

```powershell
$env:AMA_TEST_DATABASE_URL = 'postgres://ama:ama_local@localhost:5432/ama?sslmode=disable'
go test ./... -count=1
```

Each integration test creates an isolated schema, applies migrations, and removes that schema when finished. The suite verifies room and question workflows, input limits, isolation between rooms, nonnegative counters, events, and client disconnection.

## Frontend integration

Rooms are public, and operations work without registration. Reaction counts are anonymous; the frontend remembers reaction selections in the browser. Marking questions as answered is also a public operation.

Set `WSRS_ALLOWED_ORIGINS` to the frontend addresses. The same origins are used for CORS and WebSocket validation. The server uses ping/pong, read and write limits, and removes disconnected subscribers; on shutdown, it closes connections and waits for HTTP requests.

For an existing database, run `go run ./cmd/tools/terndotenv` again: migration `003_improve_messages.sql` increases the question length limit and adds the reaction constraint without deleting rooms.

## Related project

[AMA in React](https://github.com/CauaPrates/askMeAnything-React) provides the web interface for the same application. This API supplies room, question, and reaction operations for the web client.

## Author

Developed by [Cauã Prates](https://github.com/CauaPrates).
