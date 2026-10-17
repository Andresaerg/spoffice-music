package repository

import (
	"context"
	"database/sql"
	"time"
)

const DailyYouTubeCallLimit = 100

type YouTubeQuotaRepository interface {
	ReserveCall(ctx context.Context) (int, bool, error)
	GetCallCount(ctx context.Context) (int, error)
}

type sqlYouTubeQuotaRepository struct {
	db *sql.DB
}

func NewSQLYouTubeQuotaRepository(db *sql.DB) YouTubeQuotaRepository {
	return &sqlYouTubeQuotaRepository{db: db}
}

func (r *sqlYouTubeQuotaRepository) ReserveCall(ctx context.Context) (int, bool, error) {
	callDate := time.Now().In(caracasLocation()).Format("2006-01-02")
	var calls int
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO daily_youtube_api_calls (call_date, calls)
		VALUES (?, 1)
		ON CONFLICT(call_date) DO UPDATE SET calls = calls + 1
		WHERE calls < ?
		RETURNING calls
	`, callDate, DailyYouTubeCallLimit).Scan(&calls)
	if err == sql.ErrNoRows {
		return DailyYouTubeCallLimit, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return calls, true, nil
}

func (r *sqlYouTubeQuotaRepository) GetCallCount(ctx context.Context) (int, error) {
	callDate := time.Now().In(caracasLocation()).Format("2006-01-02")
	var calls int
	err := r.db.QueryRowContext(ctx, `
		SELECT calls
		FROM daily_youtube_api_calls
		WHERE call_date = ?
	`, callDate).Scan(&calls)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return calls, err
}
