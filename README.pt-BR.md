# AMA — Ask Me Anything API

[English](README.md) | **Português (Brasil)**

Backend em **Go** para uma aplicação de perguntas e respostas em salas temáticas, com persistência em **PostgreSQL** e notificações em tempo real por **WebSocket**.

A API reúne criação de salas, envio de perguntas, contagem de reações e marcação de perguntas respondidas. As mudanças são notificadas aos clientes conectados à sala correspondente.

## Funcionalidades

- Criação, listagem e consulta de salas por identificador.
- Envio, listagem e consulta de perguntas em uma sala.
- Incremento e decremento do contador de reações de uma pergunta.
- Marcação de perguntas como respondidas.
- Assinatura de eventos por sala usando WebSocket.
- Persistência com identificadores UUID e relacionamento entre salas e perguntas.

## Tecnologias

| Tecnologia | Aplicação |
| --- | --- |
| Go | Servidor HTTP e lógica da API |
| Chi | Roteamento, identificação de requisições, CORS e recuperação de panics |
| PostgreSQL | Persistência de salas e perguntas |
| pgx | Conexão com o banco e pool de conexões |
| sqlc | Geração de código Go tipado a partir de consultas SQL |
| Gorilla WebSocket | Conexões e envio de eventos em tempo real |
| Tern | Migrações do banco de dados |
| godotenv | Carregamento da configuração pelo arquivo `.env` |
| Docker | Ambiente local do banco de dados |

## Executar localmente

Pré-requisitos: Git, **Go 1.22.5 ou superior**, Docker em execução e [Tern](https://github.com/jackc/tern) disponível no `PATH`.

### 1. Clonar o repositório

```bash
git clone https://github.com/CauaPrates/askMeAnything-Go.git
cd askMeAnything-Go
go mod download
```

### 2. Configurar o ambiente

Use `.env.example` como referência para criar ou configurar o `.env` na raiz com os dados do seu PostgreSQL. Exemplo para desenvolvimento local:

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

| Variável | Finalidade |
| --- | --- |
| `WSRS_DATABASE_HOST` | Endereço do PostgreSQL |
| `WSRS_DATABASE_PORT` | Porta de conexão com o banco |
| `WSRS_DATABASE_NAME` | Nome do banco de dados |
| `WSRS_DATABASE_USER` | Usuário do banco |
| `WSRS_DATABASE_PASSWORD` | Senha do banco |
| `WSRS_DATABASE_SSLMODE` | Modo SSL do PostgreSQL; `disable` no exemplo local, `prefer` quando não definido |
| `WSRS_HTTP_ADDRESS` | Endereço do servidor, padrão `:8080` |
| `WSRS_ALLOWED_ORIGINS` | Origens permitidas para o frontend, separadas por vírgula |

Os valores acima são exemplos locais. O servidor e a ferramenta de migração carregam o `.env` a partir do diretório em que são executados.

### 3. Iniciar o PostgreSQL

Com os valores definidos no `.env`:

```bash
docker compose up -d --wait db
```

O Compose usa PostgreSQL 16, persistência em volume e verificação de saúde. O volume segue o [layout dessa versão da imagem oficial](https://hub.docker.com/_/postgres). Para iniciar também o pgAdmin:

```bash
docker compose --profile tools up -d --wait
```

O pgAdmin fica acessível em `http://localhost:8082`. As configurações desse serviço estão no `compose.yml`.

### 4. Aplicar as migrações

Uma opção de instalação do Tern com versão definida:

```bash
go install github.com/jackc/tern/v2@v2.2.0
```

Com o executável `tern` disponível no `PATH`, execute a ferramenta incluída no projeto:

```bash
go run ./cmd/tools/terndotenv
```

Ela carrega as variáveis do `.env` e aplica as migrações de `internal/store/pgstore/migrations/` usando `tern.conf`.

### 5. Iniciar a API

```bash
go run ./cmd/wsrs
```

A API atende em **http://localhost:8080**. Para consultar as salas:

```bash
curl http://localhost:8080/api/rooms/
```

## Endpoints

Base HTTP: `http://localhost:8080`.

| Método | Rota | Operação |
| --- | --- | --- |
| `GET` | `/health` | Verificar se o processo HTTP está ativo |
| `POST` | `/api/rooms/` | Criar sala |
| `GET` | `/api/rooms/` | Listar salas |
| `GET` | `/api/rooms/{room_id}/` | Consultar sala |
| `POST` | `/api/rooms/{room_id}/messages/` | Enviar pergunta |
| `GET` | `/api/rooms/{room_id}/messages/` | Listar perguntas da sala |
| `GET` | `/api/rooms/{room_id}/messages/{message_id}/` | Consultar pergunta |
| `PATCH` | `/api/rooms/{room_id}/messages/{message_id}/react` | Adicionar uma reação |
| `DELETE` | `/api/rooms/{room_id}/messages/{message_id}/react` | Remover uma reação |
| `PATCH` | `/api/rooms/{room_id}/messages/{message_id}/answer` | Marcar como respondida |
| `GET` com upgrade WebSocket | `/subscribe/{room_id}` | Assinar eventos da sala |

Os identificadores de salas e perguntas são UUIDs. No contrato da API, as perguntas são chamadas de `messages`.

### Criar uma sala

Envie um `POST /api/rooms/` com `Content-Type: application/json`:

```json
{
  "theme": "Perguntas sobre Go"
}
```

A resposta utiliza HTTP `201 Created` e contém o identificador criado:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000"
}
```

### Enviar uma pergunta

Envie um `POST /api/rooms/{room_id}/messages/`, substituindo `{room_id}` pelo ID retornado na criação:

```json
{
  "message": "Como usar Go?"
}
```

A resposta utiliza HTTP `201 Created` e também contém um campo `id`. O schema define até 255 caracteres para o tema da sala e até 1000 caracteres para o texto da pergunta. Espaços nas extremidades são removidos antes de persistir; entradas vazias são rejeitadas.

As operações de adicionar e remover reação retornam o contador atualizado no campo `count`.

As consultas de mensagens usam o identificador da sala em conjunto com o identificador da pergunta. As listas de perguntas priorizam as não respondidas e ordenam por quantidade de reações. O contador possui uma restrição de banco para permanecer não negativo.

Respostas de erro utilizam JSON no formato `{"error":"mensagem"}`. Identificadores malformados retornam `400`, recursos inexistentes retornam `404` e corpos sem `Content-Type: application/json` retornam `415`.

## Eventos em tempo real

Conecte um cliente WebSocket ao endereço:

```text
ws://localhost:8080/subscribe/{room_id}
```

| Evento | Conteúdo de `value` |
| --- | --- |
| `message_created` | `id` e `message` da pergunta |
| `message_reaction_increased` | `id` e `count` atualizado |
| `message_reaction_decreased` | `id` e `count` atualizado |
| `message_answered` | `id` da pergunta respondida |

Exemplo de notificação:

```json
{
  "kind": "message_created",
  "value": {
    "id": "550e8400-e29b-41d4-a716-446655440001",
    "message": "Como usar Go?"
  }
}
```

As operações HTTP alteram os dados; o WebSocket distribui os eventos aos assinantes da sala. As assinaturas são mantidas em memória pelo processo da API.

## Estrutura do projeto

```text
askMeAnything-Go/
├── cmd/
│   ├── wsrs/
│   │   └── main.go              # Entrada do servidor
│   └── tools/
│       └── terndotenv/
│           └── main.go          # Execução das migrações com .env
├── internal/
│   ├── api/
│   │   ├── api.go               # Rotas, handlers e eventos WebSocket
│   │   ├── utils.go             # Validação e respostas JSON
│   │   ├── websocket.go         # Assinaturas e ciclo de vida das conexões
│   │   └── api_test.go          # Testes HTTP e de integração
│   └── store/
│       └── pgstore/
│           ├── migrations/      # Schema e configuração do Tern
│           ├── queries/         # Consultas SQL
│           ├── sqlc.yaml        # Configuração da geração de código
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

O servidor inicializa o pool de conexões e injeta as consultas do banco nos handlers HTTP. A camada `pgstore` utiliza código gerado pelo sqlc a partir das consultas e do schema SQL.

O `compose.yml` descreve o ambiente local de PostgreSQL e o serviço opcional do pgAdmin.

## Ferramentas de desenvolvimento

| Comando | Finalidade |
| --- | --- |
| `go run ./cmd/wsrs` | Executar a API |
| `go run ./cmd/tools/terndotenv` | Aplicar migrações |
| `go build ./...` | Compilar os pacotes |
| `go vet ./...` | Executar análise estática |
| `go test ./...` | Executar os testes; integração habilitada por `AMA_TEST_DATABASE_URL` |
| `sqlc generate -f ./internal/store/pgstore/sqlc.yaml` | Gerar novamente o código das consultas |

Para regenerar consultas, instale o [sqlc](https://docs.sqlc.dev/) e execute o comando na raiz. Os arquivos gerados registram a versão `1.27.0`.

A coleção [Rooms do Postman](docs/Rooms.postman_collection.json) contém exemplos das operações HTTP. Importe-a e preencha os parâmetros de sala e pergunta com os IDs retornados pela API.

## Testes com PostgreSQL

Os testes HTTP de validação podem ser executados com `go test ./...`. Para habilitar os testes de persistência e WebSocket, defina `AMA_TEST_DATABASE_URL` com uma conexão a um PostgreSQL de teste. Exemplo em PowerShell:

```powershell
$env:AMA_TEST_DATABASE_URL = 'postgres://ama:ama_local@localhost:5432/ama?sslmode=disable'
go test ./... -count=1
```

Cada teste de integração cria um schema isolado, aplica as migrações e remove esse schema ao concluir. A suíte verifica o fluxo de salas e perguntas, limites de entrada, isolamento entre salas, contadores não negativos, eventos e desconexão de clientes.

## Integração com o frontend

As salas são públicas e as operações funcionam sem cadastro. O contador de reações é anônimo; o frontend mantém a seleção de reação no próprio navegador. A marcação de perguntas respondidas também é uma operação pública.

Configure `WSRS_ALLOWED_ORIGINS` com os endereços do frontend. As mesmas origens são usadas pelo CORS e pela validação das conexões WebSocket. O servidor usa ping/pong, limites de leitura e escrita e remove os assinantes desconectados; ao encerrar, fecha as conexões e aguarda as requisições HTTP.

Para um banco já criado, execute novamente `go run ./cmd/tools/terndotenv`: a migração `003_improve_messages.sql` amplia as perguntas e acrescenta a restrição de reações sem apagar as salas.

## Projeto relacionado

O [AMA em React](https://github.com/CauaPrates/askMeAnything-React) reúne a interface web da mesma proposta de aplicação. Esta API fornece as operações de salas, perguntas e reações para o cliente web.

## Autor

Desenvolvido por [Cauã Prates](https://github.com/CauaPrates).
