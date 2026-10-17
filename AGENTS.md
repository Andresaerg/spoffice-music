# Instructions for AI agents

## Project overview

- This is a Go web application. The entry point is `main.go`; HTTP handlers,
  request code, repositories, models, and SQLite setup live under `cmd/`.
- HTML templates are in `templates/`. The server listens on port `8000`.
- SQLite is provided by `modernc.org/sqlite`. `DATABASE_PATH` selects the
  database file; when unset, the application uses `./music.db`.
- YouTube search configuration comes from `YOUTUBE_API_KEY` and
  `YOUTUBE_API_URL`. Do not commit secrets or local database files.
- The Docker image runs as UID/GID `10001`, stores persistent data under
  `/data`, and the Compose deployment uses the prebuilt GHCR image.

## Working agreements

- Follow the existing Go formatting, package structure, naming, and error
  handling patterns. Keep changes focused on the request.
- Preserve existing behavior unless a requested change requires otherwise.
- Keep configuration and documentation consistent with `.env.example`,
  `Dockerfile`, and `compose.yaml`.
- Do not add dependencies or introduce generated/build artifacts unless the
  task requires them.
- Update the README when setup, configuration, or runtime behavior changes.

## Validation

- Run `go test ./...` for Go changes and `go vet ./...` when appropriate.
- For container changes, validate the Docker/Compose configuration where
  those tools are available.
- Report validation commands that could not be run and why.
