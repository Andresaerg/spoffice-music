package repository

const (
	existsByVideoIDQuery = `
		SELECT EXISTS (
			SELECT 1
			FROM musics
			WHERE videoId = ?
		)
	`
	findMusicByVideoIDQuery = `
		SELECT videoId, title, description, thumbnail
		FROM musics
		WHERE title LIKE ? OR description LIKE ?
		ORDER BY title
		LIMIT 5
	`
	saveMusicQuery = `
		INSERT INTO musics (videoId, title, description, thumbnail)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(videoId) DO UPDATE SET
			title = excluded.title,
			description = excluded.description,
			thumbnail = excluded.thumbnail
	`
	addToQueueInsertMusicQuery = `
		INSERT INTO musics (videoId, title, description, thumbnail)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(videoId) DO UPDATE SET
			title = excluded.title,
			description = excluded.description,
			thumbnail = excluded.thumbnail
	`
	addToQueueFindMusicIDQuery = `
		SELECT id
		FROM musics
		WHERE videoId = ?
	`
	addToQueueFindNextPositionQuery = `
		SELECT COALESCE(MAX(position), 0) + 1
		FROM daily_queue
		WHERE queue_date = ?
	`
	addToQueueInsertItemQuery = `
		INSERT INTO daily_queue (queue_date, music_id, position)
		VALUES (?, ?, ?)
		ON CONFLICT(queue_date, music_id) DO NOTHING
	`
	addRandomMusicsToQueueQuery = `
		WITH random_musics AS (
			SELECT id
			FROM musics
			WHERE NOT EXISTS (
				SELECT 1
				FROM daily_queue
				WHERE queue_date = ? AND music_id = musics.id
			)
			ORDER BY RANDOM()
			LIMIT ?
		)
		INSERT INTO daily_queue (queue_date, music_id, position)
		SELECT ?, random_musics.id,
			(SELECT COALESCE(MAX(position), 0) FROM daily_queue WHERE queue_date = ?)
				+ ROW_NUMBER() OVER (ORDER BY RANDOM())
		FROM random_musics
		WHERE ? = 0 OR NOT EXISTS (
			SELECT 1 FROM daily_queue WHERE queue_date = ?
		)
	`
	listDailyQueueQuery = `
		SELECT q.id, q.position, m.videoId, m.title, m.description, m.thumbnail, q.status
		FROM daily_queue q
		JOIN musics m ON m.id = q.music_id
		WHERE q.queue_date = ?
		ORDER BY q.position
	`
	findDailyQueueItemQuery = `
		SELECT q.id, q.position, m.videoId, m.title, m.description, m.thumbnail, q.status
		FROM daily_queue q
		JOIN musics m ON m.id = q.music_id
		WHERE q.id = ? AND q.queue_date = ?
	`
	resetQueueFromPositionQuery = `
		UPDATE daily_queue
		SET status = 'pending', started_at = NULL, finished_at = NULL
		WHERE queue_date = ? AND position >= ?
	`
	markQueueBeforePositionPlayedQuery = `
		UPDATE daily_queue
		SET status = 'played', finished_at = CURRENT_TIMESTAMP
		WHERE queue_date = ? AND position < ?
	`
	setQueueItemPlayingQuery = `
		UPDATE daily_queue
		SET status = 'playing', started_at = CURRENT_TIMESTAMP, finished_at = NULL
		WHERE id = ? AND queue_date = ?
	`
	nextQueueItemQuery = `
		SELECT q.id, q.position, m.videoId, m.title, m.description, m.thumbnail
		FROM daily_queue q
		JOIN musics m ON m.id = q.music_id
		WHERE q.queue_date = ? AND q.status = 'playing'
		ORDER BY q.position
		LIMIT 1
	`
	claimNextQueueItemQuery = `
		SELECT q.id, q.position, m.videoId, m.title, m.description, m.thumbnail
		FROM daily_queue q
		JOIN musics m ON m.id = q.music_id
		WHERE q.queue_date = ? AND q.status = 'pending'
		ORDER BY q.position
		LIMIT 1
	`
	currentQueueItemQuery = `
		SELECT q.id, q.position, m.videoId, m.title, m.description, m.thumbnail
		FROM daily_queue q
		JOIN musics m ON m.id = q.music_id
		WHERE q.queue_date = ? AND q.status = 'playing'
		ORDER BY q.position
		LIMIT 1
	`
	claimQueueItemQuery = `
		UPDATE daily_queue
		SET status = 'playing', started_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`
	claimPendingQueueItemQuery = `
		UPDATE daily_queue
		SET status = 'playing', started_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'pending'
	`
	resetPlayedQueueItemsQuery = `
		UPDATE daily_queue
		SET status = 'pending', started_at = NULL, finished_at = NULL
		WHERE queue_date = ? AND status = 'played'
	`
	markQueueItemPlayedQuery = `
		UPDATE daily_queue
		SET status = 'played', finished_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`
	cleanQueueThroughDateQuery = `
		DELETE FROM daily_queue
		WHERE queue_date <= ?
	`
)
