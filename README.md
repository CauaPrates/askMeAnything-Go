# AMA — Ask Me Anything API

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
| Chi | Roteamento e middlewares de log, identificação de requisições e recuperação de panics |
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

Crie ou configure o arquivo `.env` na raiz com os dados do seu PostgreSQL. Exemplo para desenvolvimento local:

```dotenv
WSRS_DATABASE_HOST=localhost
WSRS_DATABASE_PORT=5432
WSRS_DATABASE_NAME=ama
WSRS_DATABASE_USER=ama
WSRS_DATABASE_PASSWORD=ama_local
```

| Variável | Finalidade |
| --- | --- |
| `WSRS_DATABASE_HOST` | Endereço do PostgreSQL |
| `WSRS_DATABASE_PORT` | Porta de conexão com o banco |
| `WSRS_DATABASE_NAME` | Nome do banco de dados |
| `WSRS_DATABASE_USER` | Usuário do banco |
| `WSRS_DATABASE_PASSWORD` | Senha do banco |

Os valores acima são exemplos locais. O servidor e a ferramenta de migração carregam o `.env` a partir do diretório em que são executados.

### 3. Iniciar o PostgreSQL

Para usar os mesmos valores do exemplo:

```bash
docker run --name ama-postgres -e POSTGRES_USER=ama -e POSTGRES_PASSWORD=ama_local -e POSTGRES_DB=ama -p 127.0.0.1:5432:5432 -v ama_postgres_data:/var/lib/postgresql/data -d postgres:16
```

Este exemplo usa PostgreSQL 16 e o volume correspondente ao [layout dessa versão da imagem oficial](https://hub.docker.com/_/postgres). Aguarde o banco aceitar conexões antes de aplicar as migrações:

```bash
docker exec ama-postgres pg_isready -U ama -d ama
```

Para iniciar novamente o container em outra sessão, use `docker start ama-postgres`.

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

A resposta contém o identificador criado:

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

A resposta também contém um campo `id`. O schema define até 255 caracteres para o tema da sala e até 25 caracteres para o texto da pergunta.

As operações de adicionar e remover reação retornam o contador atualizado no campo `count`.

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
│   │   └── untils.go            # Leitura de salas e respostas JSON
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
├── compose.yml
├── gen.go
├── go.mod
└── go.sum
```

O servidor inicializa o pool de conexões e injeta as consultas do banco nos handlers HTTP. A camada `pgstore` utiliza código gerado pelo sqlc a partir das consultas e do schema SQL.

O `compose.yml` também descreve serviços de PostgreSQL e pgAdmin, como alternativa de configuração do ambiente. O passo a passo acima usa um container PostgreSQL independente.

## Ferramentas de desenvolvimento

| Comando | Finalidade |
| --- | --- |
| `go run ./cmd/wsrs` | Executar a API |
| `go run ./cmd/tools/terndotenv` | Aplicar migrações |
| `go build ./...` | Compilar os pacotes |
| `go vet ./...` | Executar análise estática |
| `sqlc generate -f ./internal/store/pgstore/sqlc.yaml` | Gerar novamente o código das consultas |

Para regenerar consultas, instale o [sqlc](https://docs.sqlc.dev/) e execute o comando na raiz. Os arquivos gerados registram a versão `1.27.0`.

A coleção [Rooms do Postman](docs/Rooms.postman_collection.json) contém exemplos das operações HTTP. Importe-a e preencha os parâmetros de sala e pergunta com os IDs retornados pela API.

## Projeto relacionado

O [AMA em React](https://github.com/CauaPrates/askMeAnything-React) reúne a interface web da mesma proposta de aplicação. Esta API fornece as operações de salas, perguntas e reações para o cliente web.

## Autor

Desenvolvido por [Cauã Prates](https://github.com/CauaPrates).
