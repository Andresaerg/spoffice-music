package room

import (
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/lmf3z/smlf/cmd/models"
)

type PlaybackStatus string

const (
	StatusPlaying PlaybackStatus = "PLAYING"
	StatusPaused  PlaybackStatus = "PAUSED"

	EngineYouTube   = "youtube"
	EngineInvidious = "invidious"
)

var (
	ErrEmptyQueue     = errors.New("room: queue is empty")
	ErrTrackNotFound  = errors.New("room: track not found in queue")
	ErrInvalidReorder = errors.New("room: queue reorder index out of bounds")
)

// Track represents a playable media item in the shared office room.
type Track struct {
	ID           string        `json:"id"`
	QueueEntryID int64         `json:"queueEntryId,omitempty"`
	Position     int           `json:"position,omitempty"`
	VideoID      string        `json:"videoId"`
	Title        string        `json:"title"`
	Author       string        `json:"author"`
	Duration     time.Duration `json:"duration"`
	ThumbnailURL string        `json:"thumbnailUrl"`
	EntryStatus  string        `json:"status,omitempty"`
}

// TrackFromQueueEntry converts a database QueueEntry into a room Track.
func TrackFromQueueEntry(e models.QueueEntry) Track {
	id := e.VideoID
	if e.ID > 0 {
		id = strconv.FormatInt(e.ID, 10)
	}
	return Track{
		ID:           id,
		QueueEntryID: e.ID,
		Position:     e.Position,
		VideoID:      e.VideoID,
		Title:        e.Title,
		Author:       e.Description,
		ThumbnailURL: e.Thumbnail,
		EntryStatus:  e.Status,
	}
}

// RoomSnapshot is an immutable point-in-time snapshot of RoomState for lock-free templ rendering.
type RoomSnapshot struct {
	CurrentTrack *Track
	Status       PlaybackStatus
	Engine       string
	StartedAt    time.Time
	PauseOffset  time.Duration
	Elapsed      time.Duration
	Queue        []Track
	QueueTotal   int
	QueueCurrent int
	SyncSeq      uint64
}

// RoomState is the centralized, thread-safe single source of truth for room playback.
// Its playback position (PauseOffset) is driven exclusively by the authoritative Host player.
type RoomState struct {
	mu           sync.RWMutex
	CurrentTrack *Track
	Status       PlaybackStatus
	Engine       string
	StartedAt    time.Time
	PauseOffset  time.Duration
	Queue        []Track
	syncSeq      uint64
}

func NewRoomState() *RoomState {
	return &RoomState{
		Status: StatusPaused,
		Engine: EngineYouTube,
		Queue:  make([]Track, 0),
	}
}

// Play starts or resumes playback from the current PauseOffset.
func (r *RoomState) Play() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.CurrentTrack == nil {
		if len(r.Queue) == 0 {
			return ErrEmptyQueue
		}
		next := r.Queue[0]
		next.EntryStatus = "playing"
		r.Queue = r.Queue[1:]
		r.CurrentTrack = &next
		r.PauseOffset = 0
	}

	if r.Status != StatusPlaying {
		r.StartedAt = time.Now().UTC().Add(-r.PauseOffset)
		r.Status = StatusPlaying
	}
	r.syncSeq++
	return nil
}

// Pause freezes playback at the current elapsed position.
func (r *RoomState) Pause() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.CurrentTrack == nil || r.Status == StatusPaused {
		return
	}

	r.PauseOffset = r.elapsedLocked()
	r.Status = StatusPaused
	r.syncSeq++
}

// UpdateFromHost synchronizes the authoritative RoomState with the Host player's real clock, engine, and state.
// Remotes strictly follow the position and engine reported by the Host.
func (r *RoomState) UpdateFromHost(videoID string, status PlaybackStatus, engine string, currentTime, duration time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if engine == EngineYouTube || engine == EngineInvidious {
		r.Engine = engine
	}

	if r.CurrentTrack == nil || (videoID != "" && r.CurrentTrack.VideoID != videoID) {
		return false
	}

	if currentTime < 0 {
		currentTime = 0
	}
	if duration > 0 {
		r.CurrentTrack.Duration = duration
		if currentTime > duration {
			currentTime = duration
		}
	}

	r.PauseOffset = currentTime
	r.StartedAt = time.Now().UTC().Add(-currentTime)
	if status == StatusPlaying || status == StatusPaused {
		r.Status = status
	}
	return true
}

// TogglePlayPause switches between PLAYING and PAUSED states atomically.
func (r *RoomState) TogglePlayPause() PlaybackStatus {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.Status == StatusPlaying {
		r.PauseOffset = r.elapsedLocked()
		r.Status = StatusPaused
		r.syncSeq++
		return r.Status
	}

	if r.CurrentTrack == nil && len(r.Queue) > 0 {
		next := r.Queue[0]
		next.EntryStatus = "playing"
		r.Queue = r.Queue[1:]
		r.CurrentTrack = &next
		r.PauseOffset = 0
	}

	if r.CurrentTrack != nil {
		r.StartedAt = time.Now().UTC().Add(-r.PauseOffset)
		r.Status = StatusPlaying
		r.syncSeq++
	}
	return r.Status
}

// Skip advances immediately to the next pending track in the queue.
func (r *RoomState) Skip() *Track {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.syncSeq++
	if len(r.Queue) == 0 {
		r.CurrentTrack = nil
		r.Status = StatusPaused
		r.PauseOffset = 0
		r.StartedAt = time.Time{}
		return nil
	}

	next := r.Queue[0]
	next.EntryStatus = "playing"
	r.Queue = r.Queue[1:]
	r.CurrentTrack = &next
	r.PauseOffset = 0
	r.StartedAt = time.Now().UTC()
	r.Status = StatusPlaying

	copyTrack := next
	return &copyTrack
}

// SetCurrentTrack sets the active track directly and starts playback from 0s.
func (r *RoomState) SetCurrentTrack(track Track) {
	r.mu.Lock()
	defer r.mu.Unlock()

	track.EntryStatus = "playing"
	r.CurrentTrack = &track
	r.PauseOffset = 0
	r.StartedAt = time.Now().UTC()
	r.Status = StatusPlaying
	r.syncSeq++
}

// AddToQueue appends a track to the queue, or starts playing immediately if the room is idle.
func (r *RoomState) AddToQueue(track Track) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.CurrentTrack == nil && len(r.Queue) == 0 {
		trackCopy := track
		trackCopy.EntryStatus = "playing"
		r.CurrentTrack = &trackCopy
		r.PauseOffset = 0
		r.StartedAt = time.Now().UTC()
		r.Status = StatusPlaying
		r.syncSeq++
		return
	}

	if track.EntryStatus == "" {
		track.EntryStatus = "pending"
	}
	r.Queue = append(r.Queue, track)
}

// RemoveFromQueue removes a track from the queue by ID (matches either Track.ID or Track.VideoID).
func (r *RoomState) RemoveFromQueue(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, t := range r.Queue {
		if t.ID == id || t.VideoID == id {
			r.Queue = append(r.Queue[:i], r.Queue[i+1:]...)
			return nil
		}
	}
	return ErrTrackNotFound
}

// ReorderQueue moves a track from oldIdx to newIdx atomically.
func (r *RoomState) ReorderQueue(oldIdx, newIdx int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	n := len(r.Queue)
	if oldIdx < 0 || oldIdx >= n || newIdx < 0 || newIdx >= n {
		return ErrInvalidReorder
	}
	if oldIdx == newIdx {
		return nil
	}

	item := r.Queue[oldIdx]
	r.Queue = append(r.Queue[:oldIdx], r.Queue[oldIdx+1:]...)
	reordered := make([]Track, 0, n)
	reordered = append(reordered, r.Queue[:newIdx]...)
	reordered = append(reordered, item)
	reordered = append(reordered, r.Queue[newIdx:]...)
	r.Queue = reordered
	return nil
}

// SyncFromEntries reconciles the in-memory RoomState with the persistent SQLite daily_queue.
// If the currently playing track is unchanged, its PauseOffset, Duration, Engine, and Status are preserved.
func (r *RoomState) SyncFromEntries(entries []models.QueueEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var activeEntry *models.QueueEntry
	queue := make([]Track, 0, len(entries))

	for i := range entries {
		e := entries[i]
		t := TrackFromQueueEntry(e)
		if e.Status == "playing" && activeEntry == nil {
			eCopy := e
			activeEntry = &eCopy
		}
		queue = append(queue, t)
	}

	r.Queue = queue

	if activeEntry == nil {
		r.CurrentTrack = nil
		r.Status = StatusPaused
		r.PauseOffset = 0
		r.StartedAt = time.Time{}
		r.syncSeq++
		return
	}

	activeTrack := TrackFromQueueEntry(*activeEntry)
	if r.CurrentTrack == nil || r.CurrentTrack.VideoID != activeTrack.VideoID || r.CurrentTrack.QueueEntryID != activeTrack.QueueEntryID {
		r.CurrentTrack = &activeTrack
		r.PauseOffset = 0
		r.StartedAt = time.Now().UTC()
		r.Status = StatusPlaying
		r.syncSeq++
	} else {
		r.CurrentTrack.Position = activeTrack.Position
		r.CurrentTrack.Title = activeTrack.Title
		r.CurrentTrack.Author = activeTrack.Author
		r.CurrentTrack.ThumbnailURL = activeTrack.ThumbnailURL
	}
}

// Elapsed returns the authoritative playback position.
// While PLAYING, it allows at most 2 seconds of interpolation beyond the Host's last reported PauseOffset
// so that if the Host lags, crashes, or switches engines, the server clock never runs away on its own.
func (r *RoomState) Elapsed() time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.elapsedLocked()
}

func (r *RoomState) elapsedLocked() time.Duration {
	if r.CurrentTrack == nil {
		return 0
	}
	if r.Status == StatusPlaying && !r.StartedAt.IsZero() {
		elapsed := time.Since(r.StartedAt)
		// Cap interpolation to at most PauseOffset + 2s so the clock cannot advance without the Host
		maxAllowed := r.PauseOffset + 2*time.Second
		if elapsed > maxAllowed {
			elapsed = r.PauseOffset
		}
		if r.CurrentTrack.Duration > 0 && elapsed > r.CurrentTrack.Duration {
			return r.CurrentTrack.Duration
		}
		if elapsed < 0 {
			return 0
		}
		return elapsed
	}
	return r.PauseOffset
}

// Snapshot returns a thread-safe deep copy of the current RoomState.
func (r *RoomState) Snapshot() RoomSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var currentCopy *Track
	if r.CurrentTrack != nil {
		c := *r.CurrentTrack
		currentCopy = &c
	}

	queueCopy := make([]Track, len(r.Queue))
	copy(queueCopy, r.Queue)

	queueCurrent := 0
	for idx, item := range queueCopy {
		if (currentCopy != nil && item.ID == currentCopy.ID) || item.EntryStatus == "playing" {
			queueCurrent = idx + 1
			break
		}
	}
	if queueCurrent == 0 && currentCopy != nil && currentCopy.Position > 0 {
		queueCurrent = currentCopy.Position
	}

	engine := r.Engine
	if engine == "" {
		engine = EngineYouTube
	}

	return RoomSnapshot{
		CurrentTrack: currentCopy,
		Status:       r.Status,
		Engine:       engine,
		StartedAt:    r.StartedAt,
		PauseOffset:  r.PauseOffset,
		Elapsed:      r.elapsedLocked(),
		Queue:        queueCopy,
		QueueTotal:   len(queueCopy),
		QueueCurrent: queueCurrent,
		SyncSeq:      r.syncSeq,
	}
}
