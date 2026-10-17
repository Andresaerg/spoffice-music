package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/lmf3z/smlf/cmd/models"
)

type QueueRepository interface {
	ExistsByVideoID(ctx context.Context, videoID string) (bool, error)
	SearchMusics(ctx context.Context, query string) ([]models.Queue, error)
	SaveMusic(ctx context.Context, song models.YTSong) error
	AddToQueue(ctx context.Context, queue models.Queue) (bool, error)
	AddRandomMusicsToQueue(ctx context.Context, count int, onlyIfEmpty bool) (int, error)
	ListDailyQueue(ctx context.Context) ([]models.QueueEntry, error)
	PlayQueueItem(ctx context.Context, id int64) (models.QueueEntry, error)
	NextQueueItem(ctx context.Context) (models.QueueEntry, error)
	ClaimNextQueueItem(ctx context.Context) (models.QueueEntry, error)
	CurrentOrClaimQueueItem(ctx context.Context) (models.QueueEntry, error)
	MarkQueueItemPlayed(ctx context.Context, id int64) error
	CleanQueueThroughDate(ctx context.Context, date string) error
}

type sqlQueueRepository struct {
	db *sql.DB
}

func NewSQLQueueRepository(db *sql.DB) QueueRepository {
	return &sqlQueueRepository{db: db}
}

func (r *sqlQueueRepository) ExistsByVideoID(ctx context.Context, videoID string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, existsByVideoIDQuery, videoID).Scan(&exists)
	return exists, err
}

func (r *sqlQueueRepository) SearchMusics(ctx context.Context, query string) ([]models.Queue, error) {
	pattern := "%" + strings.TrimSpace(query) + "%"
	rows, err := r.db.QueryContext(ctx, findMusicByVideoIDQuery, pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var songs []models.Queue
	for rows.Next() {
		var song models.Queue
		if err := rows.Scan(&song.VideoID, &song.Title, &song.Description, &song.Thumbnail); err != nil {
			return nil, err
		}
		songs = append(songs, song)
	}
	return songs, rows.Err()
}

func (r *sqlQueueRepository) SaveMusic(ctx context.Context, song models.YTSong) error {
	thumbnail := song.Thumbnails.Default.URL
	if song.Thumbnails.High != nil {
		thumbnail = song.Thumbnails.High.URL
	} else if song.Thumbnails.Standard != nil {
		thumbnail = song.Thumbnails.Standard.URL
	}

	_, err := r.db.ExecContext(
		ctx,
		saveMusicQuery,
		song.VideoID,
		song.Title,
		song.Description,
		thumbnail,
	)
	return err
}

func (r *sqlQueueRepository) AddToQueue(ctx context.Context, queue models.Queue) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(
		ctx,
		addToQueueInsertMusicQuery,
		queue.VideoID,
		queue.Title,
		queue.Description,
		queue.Thumbnail,
	)
	if err != nil {
		return false, err
	}

	var musicID int64
	err = tx.QueryRowContext(ctx, addToQueueFindMusicIDQuery, queue.VideoID).Scan(&musicID)
	if err != nil {
		return false, err
	}

	queueDate := time.Now().In(caracasLocation()).Format("2006-01-02")
	var position int
	err = tx.QueryRowContext(ctx, addToQueueFindNextPositionQuery, queueDate).Scan(&position)
	if err != nil {
		return false, err
	}

	result, err := tx.ExecContext(ctx, addToQueueInsertItemQuery, queueDate, musicID, position)
	if err != nil {
		return false, err
	}
	added, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return added > 0, nil
}

func (r *sqlQueueRepository) AddRandomMusicsToQueue(ctx context.Context, count int, onlyIfEmpty bool) (int, error) {
	queueDate := time.Now().In(caracasLocation()).Format("2006-01-02")
	onlyIfEmptyValue := 0
	if onlyIfEmpty {
		onlyIfEmptyValue = 1
	}

	result, err := r.db.ExecContext(
		ctx,
		addRandomMusicsToQueueQuery,
		queueDate,
		count,
		queueDate,
		queueDate,
		onlyIfEmptyValue,
		queueDate,
	)
	if err != nil {
		return 0, err
	}

	added, err := result.RowsAffected()
	return int(added), err
}

func (r *sqlQueueRepository) ListDailyQueue(ctx context.Context) ([]models.QueueEntry, error) {
	queueDate := time.Now().In(caracasLocation()).Format("2006-01-02")
	rows, err := r.db.QueryContext(ctx, listDailyQueueQuery, queueDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]models.QueueEntry, 0)
	for rows.Next() {
		var entry models.QueueEntry
		if err := rows.Scan(
			&entry.ID,
			&entry.Position,
			&entry.VideoID,
			&entry.Title,
			&entry.Description,
			&entry.Thumbnail,
			&entry.Status,
		); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (r *sqlQueueRepository) PlayQueueItem(ctx context.Context, id int64) (models.QueueEntry, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.QueueEntry{}, err
	}
	defer tx.Rollback()

	queueDate := time.Now().In(caracasLocation()).Format("2006-01-02")
	var entry models.QueueEntry
	err = tx.QueryRowContext(ctx, findDailyQueueItemQuery, id, queueDate).Scan(
		&entry.ID,
		&entry.Position,
		&entry.VideoID,
		&entry.Title,
		&entry.Description,
		&entry.Thumbnail,
		&entry.Status,
	)
	if err != nil {
		return models.QueueEntry{}, err
	}

	if _, err := tx.ExecContext(ctx, resetQueueFromPositionQuery, queueDate, entry.Position); err != nil {
		return models.QueueEntry{}, err
	}
	if _, err := tx.ExecContext(ctx, markQueueBeforePositionPlayedQuery, queueDate, entry.Position); err != nil {
		return models.QueueEntry{}, err
	}
	if _, err := tx.ExecContext(ctx, setQueueItemPlayingQuery, id, queueDate); err != nil {
		return models.QueueEntry{}, err
	}

	if err := tx.Commit(); err != nil {
		return models.QueueEntry{}, err
	}
	entry.Status = "playing"
	return entry, nil
}

func (r *sqlQueueRepository) NextQueueItem(ctx context.Context) (models.QueueEntry, error) {
	queueDate := time.Now().In(caracasLocation()).Format("2006-01-02")
	var entry models.QueueEntry
	err := r.db.QueryRowContext(ctx, nextQueueItemQuery, queueDate).Scan(
		&entry.ID,
		&entry.Position,
		&entry.VideoID,
		&entry.Title,
		&entry.Description,
		&entry.Thumbnail,
	)
	if err == sql.ErrNoRows {
		return models.QueueEntry{}, nil
	}
	return entry, err
}

func (r *sqlQueueRepository) ClaimNextQueueItem(ctx context.Context) (models.QueueEntry, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.QueueEntry{}, err
	}
	defer tx.Rollback()

	queueDate := time.Now().In(caracasLocation()).Format("2006-01-02")
	var entry models.QueueEntry
	err = tx.QueryRowContext(ctx, claimNextQueueItemQuery, queueDate).Scan(
		&entry.ID,
		&entry.Position,
		&entry.VideoID,
		&entry.Title,
		&entry.Description,
		&entry.Thumbnail,
	)
	if err == sql.ErrNoRows {
		return models.QueueEntry{}, nil
	}
	if err != nil {
		return models.QueueEntry{}, err
	}

	_, err = tx.ExecContext(ctx, claimQueueItemQuery, entry.ID)
	if err != nil {
		return models.QueueEntry{}, err
	}
	return entry, tx.Commit()
}

func (r *sqlQueueRepository) CurrentOrClaimQueueItem(ctx context.Context) (models.QueueEntry, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.QueueEntry{}, err
	}
	defer tx.Rollback()

	queueDate := time.Now().In(caracasLocation()).Format("2006-01-02")
	var entry models.QueueEntry
	err = tx.QueryRowContext(ctx, currentQueueItemQuery, queueDate).Scan(
		&entry.ID,
		&entry.Position,
		&entry.VideoID,
		&entry.Title,
		&entry.Description,
		&entry.Thumbnail,
	)
	if err == nil {
		return entry, tx.Commit()
	}
	if err != sql.ErrNoRows {
		return models.QueueEntry{}, err
	}

	err = tx.QueryRowContext(ctx, claimNextQueueItemQuery, queueDate).Scan(
		&entry.ID,
		&entry.Position,
		&entry.VideoID,
		&entry.Title,
		&entry.Description,
		&entry.Thumbnail,
	)
	if err == sql.ErrNoRows {
		if _, err := tx.ExecContext(ctx, resetPlayedQueueItemsQuery, queueDate); err != nil {
			return models.QueueEntry{}, err
		}
		err = tx.QueryRowContext(ctx, claimNextQueueItemQuery, queueDate).Scan(
			&entry.ID,
			&entry.Position,
			&entry.VideoID,
			&entry.Title,
			&entry.Description,
			&entry.Thumbnail,
		)
		if err == sql.ErrNoRows {
			return models.QueueEntry{}, tx.Commit()
		}
	}
	if err != nil {
		return models.QueueEntry{}, err
	}

	result, err := tx.ExecContext(ctx, claimPendingQueueItemQuery, entry.ID)
	if err != nil {
		return models.QueueEntry{}, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return models.QueueEntry{}, err
	}
	if updated == 0 {
		if err := tx.Commit(); err != nil {
			return models.QueueEntry{}, err
		}
		return r.CurrentOrClaimQueueItem(ctx)
	}
	return entry, tx.Commit()
}

func (r *sqlQueueRepository) MarkQueueItemPlayed(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, markQueueItemPlayedQuery, id)
	return err
}

func (r *sqlQueueRepository) CleanQueueThroughDate(ctx context.Context, date string) error {
	_, err := r.db.ExecContext(ctx, cleanQueueThroughDateQuery, date)
	return err
}

func caracasLocation() *time.Location {
	location, err := time.LoadLocation("America/Caracas")
	if err != nil {
		return time.FixedZone("VET", -4*60*60)
	}
	return location
}
