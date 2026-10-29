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
	ListSavedMusics(ctx context.Context, limit int) ([]models.YTSong, error)
	SaveMusic(ctx context.Context, song models.YTSong) error
	AddToQueue(ctx context.Context, queue models.Queue) (bool, error)
	AddToQueueNext(ctx context.Context, queue models.Queue) (bool, error)
	AddRandomMusicsToQueue(ctx context.Context, count int, onlyIfEmpty bool) (int, error)
	ListDailyQueue(ctx context.Context) ([]models.QueueEntry, error)
	PlayQueueItem(ctx context.Context, id int64) (models.QueueEntry, error)
	NextQueueItem(ctx context.Context) (models.QueueEntry, error)
	ClaimNextQueueItem(ctx context.Context) (models.QueueEntry, error)
	CurrentOrClaimQueueItem(ctx context.Context) (models.QueueEntry, error)
	MarkQueueItemPlayed(ctx context.Context, id int64) error
	CleanQueueThroughDate(ctx context.Context, date string) error
	RemoveQueueItem(ctx context.Context, id int64) error
	ClearPlayedQueueItems(ctx context.Context) (int, error)
	MoveQueueItemNext(ctx context.Context, id int64) error
	ReorderQueue(ctx context.Context, ids []int64) error
	CountQueue(ctx context.Context) (int, error)
	CurrentPlayingPosition(ctx context.Context) (int, error)
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

func (r *sqlQueueRepository) ListSavedMusics(ctx context.Context, limit int) ([]models.YTSong, error) {
	if limit <= 0 {
		limit = 12
	}
	rows, err := r.db.QueryContext(ctx, listSavedMusicsQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var songs []models.YTSong
	for rows.Next() {
		var videoID, title, description, thumbnail string
		if err := rows.Scan(&videoID, &title, &description, &thumbnail); err != nil {
			return nil, err
		}
		songs = append(songs, models.YTSong{
			VideoID:     videoID,
			Title:       title,
			Description: description,
			Thumbnails:  models.Thumbnails{Default: models.Thumbnail{URL: thumbnail}},
		})
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

	var existingID int64
	var existingStatus string
	err = tx.QueryRowContext(ctx, "SELECT id, status FROM daily_queue WHERE queue_date = ? AND music_id = ?", queueDate, musicID).Scan(&existingID, &existingStatus)
	if err == nil {
		if existingStatus != "played" {
			return false, nil
		}
		var nextPosition int
		if err := tx.QueryRowContext(ctx, addToQueueFindNextPositionQuery, queueDate).Scan(&nextPosition); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE daily_queue SET position = ?, status = 'pending', started_at = NULL, finished_at = NULL WHERE id = ?", nextPosition, existingID); err != nil {
			return false, err
		}
		if err := normalizeQueuePositionsTx(ctx, tx, queueDate); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return true, nil
	} else if err != sql.ErrNoRows {
		return false, err
	}

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

func (r *sqlQueueRepository) AddToQueueNext(ctx context.Context, queue models.Queue) (bool, error) {
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

	var existingID int64
	var existingStatus string
	err = tx.QueryRowContext(ctx, "SELECT id, status FROM daily_queue WHERE queue_date = ? AND music_id = ?", queueDate, musicID).Scan(&existingID, &existingStatus)
	if err == nil {
		if existingStatus != "played" {
			return false, nil
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM daily_queue WHERE id = ?", existingID); err != nil {
			return false, err
		}
		if err := normalizeQueuePositionsTx(ctx, tx, queueDate); err != nil {
			return false, err
		}
	} else if err != sql.ErrNoRows {
		return false, err
	}

	var targetPosition int
	var playingPosition int
	err = tx.QueryRowContext(ctx, "SELECT position FROM daily_queue WHERE queue_date = ? AND status = 'playing' LIMIT 1", queueDate).Scan(&playingPosition)
	if err == nil && playingPosition > 0 {
		targetPosition = playingPosition + 1
	} else {
		var lastPlayedPosition sql.NullInt64
		err = tx.QueryRowContext(ctx, "SELECT MAX(position) FROM daily_queue WHERE queue_date = ? AND status = 'played'", queueDate).Scan(&lastPlayedPosition)
		if err == nil && lastPlayedPosition.Valid && lastPlayedPosition.Int64 > 0 {
			targetPosition = int(lastPlayedPosition.Int64) + 1
		} else {
			targetPosition = 1
		}
	}

	if _, err := tx.ExecContext(ctx, "UPDATE daily_queue SET position = -(position + 100000) WHERE queue_date = ? AND position >= ?", queueDate, targetPosition); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE daily_queue SET position = (-position - 100000) + 1 WHERE queue_date = ? AND position < 0", queueDate); err != nil {
		return false, err
	}

	_, err = tx.ExecContext(ctx, "INSERT INTO daily_queue (queue_date, music_id, position, status) VALUES (?, ?, ?, 'pending')", queueDate, musicID, targetPosition)
	if err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
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
		// Smart Auto-DJ: before looping already-played tracks, try pulling up to 5 fresh unplayed tracks from musics
		_, _ = tx.ExecContext(
			ctx,
			addRandomMusicsToQueueQuery,
			queueDate,
			5,
			queueDate,
			queueDate,
			0,
			queueDate,
		)
		err = tx.QueryRowContext(ctx, claimNextQueueItemQuery, queueDate).Scan(
			&entry.ID,
			&entry.Position,
			&entry.VideoID,
			&entry.Title,
			&entry.Description,
			&entry.Thumbnail,
		)
	}
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
	_, _ = r.db.ExecContext(ctx, incrementMusicPlayCountForQueueItemQuery, id)
	_, err := r.db.ExecContext(ctx, markQueueItemPlayedQuery, id)
	return err
}

func (r *sqlQueueRepository) CleanQueueThroughDate(ctx context.Context, date string) error {
	_, err := r.db.ExecContext(ctx, cleanQueueThroughDateQuery, date)
	return err
}

func (r *sqlQueueRepository) RemoveQueueItem(ctx context.Context, id int64) error {
	queueDate := time.Now().In(caracasLocation()).Format("2006-01-02")
	_, err := r.db.ExecContext(ctx, removeQueueItemQuery, id, queueDate)
	return err
}

func (r *sqlQueueRepository) ClearPlayedQueueItems(ctx context.Context) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	queueDate := time.Now().In(caracasLocation()).Format("2006-01-02")
	res, err := tx.ExecContext(ctx, "DELETE FROM daily_queue WHERE queue_date = ? AND status = 'played'", queueDate)
	if err != nil {
		return 0, err
	}
	removed, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if removed > 0 {
		if err := normalizeQueuePositionsTx(ctx, tx, queueDate); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(removed), nil
}

func (r *sqlQueueRepository) MoveQueueItemNext(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	queueDate := time.Now().In(caracasLocation()).Format("2006-01-02")

	rows, err := tx.QueryContext(ctx, "SELECT id, status FROM daily_queue WHERE queue_date = ? ORDER BY position", queueDate)
	if err != nil {
		return err
	}
	type itemInfo struct {
		id     int64
		status string
	}
	var items []itemInfo
	targetIdx := -1
	for rows.Next() {
		var it itemInfo
		if err := rows.Scan(&it.id, &it.status); err != nil {
			rows.Close()
			return err
		}
		idx := len(items)
		if it.id == id {
			targetIdx = idx
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if targetIdx == -1 || items[targetIdx].status == "playing" {
		return nil
	}

	if items[targetIdx].status == "played" {
		if _, err := tx.ExecContext(ctx, "UPDATE daily_queue SET status = 'pending', started_at = NULL, finished_at = NULL WHERE id = ?", id); err != nil {
			return err
		}
	}

	targetItem := items[targetIdx]
	remaining := make([]itemInfo, 0, len(items)-1)
	for i, it := range items {
		if i != targetIdx {
			remaining = append(remaining, it)
		}
	}

	insertIdx := 0
	for i, it := range remaining {
		if it.status == "playing" {
			insertIdx = i + 1
			break
		}
		if it.status == "played" {
			insertIdx = i + 1
		}
	}

	orderedIDs := make([]int64, 0, len(items))
	for i, it := range remaining {
		if i == insertIdx {
			orderedIDs = append(orderedIDs, targetItem.id)
		}
		orderedIDs = append(orderedIDs, it.id)
	}
	if insertIdx >= len(remaining) {
		orderedIDs = append(orderedIDs, targetItem.id)
	}

	if _, err := tx.ExecContext(ctx, "UPDATE daily_queue SET position = -(position + 100000) WHERE queue_date = ?", queueDate); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, updateQueueItemPositionQuery)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, itemID := range orderedIDs {
		if _, err := stmt.ExecContext(ctx, i+1, itemID, queueDate); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func normalizeQueuePositionsTx(ctx context.Context, tx *sql.Tx, queueDate string) error {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM daily_queue WHERE queue_date = ? ORDER BY position, id", queueDate)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if len(ids) == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE daily_queue SET position = -(position + 100000) WHERE queue_date = ?", queueDate); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, updateQueueItemPositionQuery)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, id := range ids {
		if _, err := stmt.ExecContext(ctx, i+1, id, queueDate); err != nil {
			return err
		}
	}
	return nil
}

func (r *sqlQueueRepository) ReorderQueue(ctx context.Context, ids []int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	queueDate := time.Now().In(caracasLocation()).Format("2006-01-02")

	// Set temporary negative positions to prevent UNIQUE(queue_date, position) collisions
	if _, err := tx.ExecContext(ctx, "UPDATE daily_queue SET position = -(position + 100000) WHERE queue_date = ?", queueDate); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, updateQueueItemPositionQuery)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, id := range ids {
		if _, err := stmt.ExecContext(ctx, i+1, id, queueDate); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *sqlQueueRepository) CountQueue(ctx context.Context) (int, error) {
	queueDate := time.Now().In(caracasLocation()).Format("2006-01-02")
	var count int
	err := r.db.QueryRowContext(ctx, countQueueQuery, queueDate).Scan(&count)
	return count, err
}

func (r *sqlQueueRepository) CurrentPlayingPosition(ctx context.Context) (int, error) {
	queueDate := time.Now().In(caracasLocation()).Format("2006-01-02")
	var position int
	err := r.db.QueryRowContext(ctx, currentPlayingPositionQuery, queueDate).Scan(&position)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return position, err
}

func caracasLocation() *time.Location {
	location, err := time.LoadLocation("America/Caracas")
	if err != nil {
		return time.FixedZone("VET", -4*60*60)
	}
	return location
}
