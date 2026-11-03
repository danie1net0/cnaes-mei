# AGENTS.md

Regras deste projeto para qualquer agente (Claude Code, Codex, Copilot).

## O que é

CLI em Go que coleta as ocupações permitidas para o MEI, e o CNAE de cada uma,
da listagem do gov.br, e entrega em CSV, TSV, JSON ou numa tabela do
PostgreSQL. O `README.md` explica o uso; aqui ficam só as regras de quem mexe
no código.

## Estrutura

- `internal/mei` — domínio: o tipo `Activity`, a leitura do CNAE, o parser de
  uma página e o scraper. Não conhece saída nem banco.
- `internal/output` — escreve CSV, TSV e JSON.
- `internal/postgres` — grava na tabela, substituindo o conteúdo numa
  transação.
- `internal/cli` — amarra os três e lê as opções da linha de comando.
- `main.go` — só trata o sinal de interrupção e o código de saída.

Dependência só aponta para dentro: `cli` conhece os outros três, e os outros
três não conhecem o `cli`.

## Regras

- **Go 1.26**, sem `GOEXPERIMENT`.
- **Teste não acessa a internet.** Página vem de `httptest` ou de
  `internal/mei/testdata`. Teste que precisa do PostgreSQL é pulado quando
  `TEST_POSTGRES_URL` não está definida.
- **Cobertura de 100% das linhas** em todo código novo ou alterado, medida com
  `go test -coverprofile`. Código que não dá para alcançar não recebe exceção:
  ele é removido.
- **Erro sempre embrulhado com contexto** (`fmt.Errorf("... %s: %w", ...)`), e
  erro que o chamador precisa reconhecer vira sentinela exportada
  (`ErrNoTable`, `ErrTooFewActivities`).
- **Nada de log no domínio sem `slog`**: o `Scraper` recebe um `*slog.Logger`
  e, por padrão, descarta.
- **Mudou a fonte, muda a amostra:** se a listagem do gov.br mudar de HTML,
  atualize `internal/mei/testdata/letter-a.html` com um recorte do HTML real,
  não com um inventado.
- Antes de entregar: `gofmt -s -l .` sem saída, `go vet ./...` limpo e
  `go test --race ./...` passando.

## Idioma

Código, comentários, nomes de teste e mensagens de erro em inglês. `README.md`,
este arquivo e mensagens ao usuário final em português.
