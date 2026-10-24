# spoffice-music

Aplicación web para buscar música en YouTube y administrar una cola diaria.
El servidor escucha en el puerto `8000` y guarda la base de datos SQLite en
`DATABASE_PATH`.

## Requisitos

- Para ejecutar localmente: Go `1.26.5` o compatible con la versión indicada
  en [`go.mod`](./go.mod).
- Para contenedores: Docker Engine; Docker Compose v2 para el despliegue con
  Compose.

## Configuración

Crea un archivo `.env` a partir de la plantilla:

```sh
cp .env.example .env
```

Edita `.env` y configura:

| Variable          | Descripción                                                                                                      |
| ----------------- | ---------------------------------------------------------------------------------------------------------------- |
| `DATABASE_PATH`   | Ruta del archivo de SQLite. Usa `./data/music.db` para ejecución local y `/data/music.db` dentro del contenedor. |
| `INVIDIOUS_HOST`  | (Opcional) URL de la instancia de Invidious. Por defecto: `https://invidious.f5.si`.                             |


## Ejecución local con Go

Para guardar la base de datos dentro del proyecto, crea el directorio y cambia
`DATABASE_PATH` en `.env` a `./data/music.db`:

```sh
mkdir -p data
```

Descarga las dependencias e inicia el servidor:

```sh
go mod download
go run .
```

Abre <http://localhost:8000>.

## Construir y ejecutar con Docker

Construye la imagen desde el código fuente:

```sh
docker build -t spoffice-music:latest .
```

Ejecuta el contenedor con una base de datos persistente en un volumen Docker:

```sh
docker run -p 8000:8000 --env-file .env -v "$(pwd)/data:/data" --name spoffice-music spoffice-music:latest
```

En este caso, `DATABASE_PATH` en `.env` debe ser `/data/music.db`. Para detener
y volver a iniciar el contenedor:

```sh
docker stop spoffice-music
docker start spoffice-music
```

El volumen `spoffice-music-data` conserva la base de datos aunque se elimine el
contenedor. Si prefieres guardar los datos en una carpeta del host, sustituye
el volumen por `-v "$(pwd)/data:/data"` y asegura que el directorio sea
escribible por el usuario del contenedor (UID/GID `10001`):

```sh
mkdir -p data
sudo chown -R 10001:10001 data
```

## Desplegar con Docker Compose

El archivo [`compose.yaml`](./compose.yaml) usa la imagen precompilada
`ghcr.io/lmf3z/spoffice:latest`, carga las variables desde `.env`, publica el
puerto `8000` y conserva SQLite en el volumen `music-data`.

Prepara `.env` como se indica en [Configuración](#configuración) y establece
`DATABASE_PATH=/data/music.db`. Si la imagen de GHCR es privada, autentícate
antes de descargarla:

```sh
docker login ghcr.io
```

Descarga la imagen y levanta el servicio:

```sh
docker compose pull
docker compose up -d
```

Consulta los registros o detén la aplicación con:

```sh
docker compose logs -f
docker compose down
```

`docker compose down` conserva el volumen de datos. Para borrar también la
base de datos y los demás datos persistidos, ejecuta `docker compose down -v`.

## Comprobaciones para desarrollo

Ejecuta las pruebas y el análisis estático de Go con:

```sh
go test ./...
go vet ./...
```
