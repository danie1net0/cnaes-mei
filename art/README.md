# Artes

Pixel art 16-bit: o carrinho de vendedor é o microempreendedor, o gopher segura
a lista de ocupações com as marcas de conferido, e o banco de dados é onde a
coleta termina.

| Arquivo | Tamanho | Onde é usado |
|---|---|---|
| [`avatar-1024.png`](avatar-1024.png) | 1024×1024 | Original, de onde saem os outros tamanhos |
| [`avatar-256.png`](avatar-256.png) | 256×256 | Avatar do projeto no GitLab |
| [`social-preview-1280x640.png`](social-preview-1280x640.png) | 1280×640 | *Social preview* do GitHub, a imagem que aparece quando o link é compartilhado |

Paleta: fundo grafite `#242b33`, objetos em marfim com sombra cinza-ardósia,
destaques em coral `#dd5e53` e o ciano do Go no gopher.

O avatar de 256 px é o que o GitLab aceita (o limite é 200 KB). Para gerar
outro tamanho a partir do original:

```console
$ sips -Z 256 avatar-1024.png --out avatar-256.png
```

O *social preview* saiu de uma arte 21:9 recortada para 2:1 — o formato que o
GitHub recomenda. Nenhum dos dois leva o nome do projeto: o nome já vem ao lado
da imagem nos dois sites.

## Como republicar

Avatar no GitLab, por `multipart/form-data` (o `--field avatar=@arquivo` não
serve):

```console
$ glab api --method PUT projects/88455035 \
    --header 'Content-Type: multipart/form-data; boundary=BOUNDARY' \
    --input corpo.multipart --silent
```

O *social preview* do GitHub não tem API: só o formulário em
*Settings → General → Social preview*.
