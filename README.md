# cnaes-mei

Coleta as ocupações permitidas para o MEI, e o CNAE de cada uma, a partir da
[listagem publicada no gov.br][listagem], e entrega o resultado em CSV, TSV,
JSON ou direto numa tabela do PostgreSQL.

A Receita Federal não publica essa relação junto com os dados do CNPJ: ela sai
em páginas HTML, uma por letra inicial da ocupação. Este projeto existe para
cruzar essas ocupações com uma base de CNPJs — por exemplo, para saber quais
estabelecimentos exercem alguma atividade que um MEI poderia exercer.

Na coleta de 02/11/2026 eram **470 ocupações** e **350 CNAEs distintos** (o
mesmo CNAE serve a várias ocupações), em cerca de 2 segundos.

[listagem]: https://www.gov.br/empresas-e-negocios/pt-br/empreendedor/quero-ser-mei/atividades-permitidas

## Instalação

Requer [Go](https://go.dev/) 1.26 ou mais novo.

```console
$ go build -o cnaes-mei .
```

Ou, para instalar no `GOPATH`:

```console
$ go install gitlab.com/danie1net0/cnaes-mei@latest
```

## Uso

Por padrão escreve CSV na saída padrão:

```console
$ cnaes-mei
cnae,ocupacao,letra
0121101,VIVEIRISTA INDEPENDENTE,V
0159801,APICULTOR(A) INDEPENDENTE,A
...
```

Num arquivo, no formato que o `COPY` do PostgreSQL lê:

```console
$ cnaes-mei -format tsv -output cnaes-mei.tsv
```

Direto no banco, substituindo o conteúdo da tabela numa única transação:

```console
$ cnaes-mei -database-url "postgres://usuario@localhost:5432/minha_receita?sslmode=disable"
```

Passando `-output` junto com `-database-url`, faz as duas coisas.

### Opções

| Opção | Padrão | O que faz |
|---|---|---|
| `-format` | `csv` | Formato da saída: `csv`, `tsv` ou `json` |
| `-output` | `-` | Arquivo de saída, ou `-` para a saída padrão |
| `-database-url` | `$DATABASE_URL` | URI do PostgreSQL onde gravar |
| `-table` | `cnae_mei` | Tabela onde gravar |
| `-base-url` | `$MEI_BASE_URL` | Endereço da listagem |
| `-min` | `400` | Falha se vierem menos atividades que isso; `0` desliga |
| `-strict` | `false` | Falha em vez de pular uma linha ilegível |
| `-parallel` | `4` | Páginas buscadas ao mesmo tempo |
| `-attempts` | `3` | Tentativas por página que falha |
| `-timeout` | `30s` | Tempo limite de cada requisição |
| `-verbose` | `false` | Registra cada letra lida |
| `-version` | | Mostra a versão e sai |

## O que a ferramenta faz com a fonte

A listagem não tem índice navegável, então as 26 letras do alfabeto são
tentadas e as que respondem 404 são ignoradas — hoje são 19 páginas. Assim,
uma letra que a fonte ganhe ou perca não exige mudança no código.

**O CNAE sai com sete dígitos, como texto.** Em número, códigos como
`0111-3/01` perderiam o zero à esquerda. No PostgreSQL ele é gravado como
`int`, que é o tipo usado nas bases de CNPJ; o zero volta na formatação.

**Linha ilegível não derruba a coleta.** A própria listagem publica CNAE com
dígito faltando — em 02/11/2026, `602-5/02` para `DEPILADOR(A) INDEPENDENTE`,
que deveria ser `9602-5/02`. Essas linhas são registradas como aviso e
puladas; com `-strict`, a coleta para na primeira.

**O tamanho da colheita é conferido.** Se a página mudar de estrutura e as
tabelas passarem a vir vazias, tudo continua respondendo 200 e nada quebra
sozinho — por isso a coleta falha quando traz menos que `-min` atividades.

**A mesma coleta gera sempre o mesmo arquivo.** O resultado é ordenado por
CNAE e ocupação, então dá para versionar a saída e enxergar no `diff` o que a
fonte mudou de um mês para o outro.

## A tabela no PostgreSQL

Criada automaticamente na primeira execução:

```sql
CREATE TABLE cnae_mei (
    cnae     int NOT NULL,
    ocupacao text NOT NULL,
    letra    char(1) NOT NULL,
    PRIMARY KEY (cnae, ocupacao)
);
```

São guardados todos os pares — a mesma ocupação pode aparecer sob CNAEs
diferentes e o mesmo CNAE serve a várias ocupações. Para a pergunta "este CNAE
é permitido ao MEI?", o que importa são os CNAEs distintos:

```sql
SELECT count(DISTINCT cnae) FROM cnae_mei;
```

E o cruzamento com uma base de CNPJs no modelo relacional da
[minha-receita](https://codeberg.org/cuducos/minha-receita) — considerando a
atividade principal e as secundárias:

```sql
SELECT DISTINCT e.cnpj
FROM estabelecimento e
LEFT JOIN estabelecimento_cnae ec ON ec.cnpj = e.cnpj
WHERE e.cnae_fiscal IN (SELECT cnae FROM cnae_mei)
   OR ec.cnae IN (SELECT cnae FROM cnae_mei);
```

## Desenvolvimento

```console
$ go test ./...                 # testes
$ go test --race ./...          # como na CI
$ go vet ./...
$ gofmt -s -l .                 # não deve listar nada
```

Os testes que escrevem no PostgreSQL são pulados quando `TEST_POSTGRES_URL`
não está definida:

```console
$ TEST_POSTGRES_URL="postgres://usuario@localhost:5432/postgres?sslmode=disable" go test ./...
```

Nenhum teste acessa a internet: as páginas são servidas por um `httptest` e
há uma amostra do HTML real em `internal/mei/testdata`.

Para gravar a versão no binário:

```console
$ go build -ldflags "-X gitlab.com/danie1net0/cnaes-mei/internal/cli.Version=$(git describe --tags --always)" -o cnaes-mei .
```
