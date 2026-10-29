package database

import (
	"database/sql"
	"os"

	_ "modernc.org/sqlite"
)

func Connect() *sql.DB {
	path := os.Getenv("DATABASE_PATH")
	if path == "" {
		path = "./music.db"
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		panic(err)
	}

	_, _ = db.Exec("PRAGMA journal_mode = WAL;")
	_, _ = db.Exec("PRAGMA busy_timeout = 5000;")

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS musics (
			id INTEGER PRIMARY KEY,
			videoId TEXT NOT NULL UNIQUE,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			thumbnail TEXT NOT NULL DEFAULT '',
			play_count INTEGER NOT NULL DEFAULT 0,
			last_played_at TEXT
		)
	`)

	if err != nil {
		panic(err)
	}

	// Safe non-destructive migration for existing databases
	_, _ = db.Exec("ALTER TABLE musics ADD COLUMN play_count INTEGER NOT NULL DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE musics ADD COLUMN last_played_at TEXT")

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS daily_queue (
			id INTEGER PRIMARY KEY,
			queue_date TEXT NOT NULL,
			music_id INTEGER NOT NULL,
			position INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			started_at TEXT,
			finished_at TEXT,
			FOREIGN KEY (music_id) REFERENCES musics(id),
			UNIQUE(queue_date, music_id),
			UNIQUE(queue_date, position)
		)
	`)
	if err != nil {
		panic(err)
	}


	_, err = db.Exec("CREATE INDEX IF NOT EXISTS idx_daily_queue_order ON daily_queue(queue_date, status, position)")
	if err != nil {
		panic(err)
	}

	_, err = db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_musics_video_id ON musics(videoId)")
	if err != nil {
		panic(err)
	}

	return db
}
