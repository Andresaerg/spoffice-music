package room

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/lmf3z/smlf/cmd/api/repository"
)

// ComponentRenderer renders a RoomSnapshot into a compiled templ.Component.
type ComponentRenderer func(snapshot RoomSnapshot) templ.Component

type subscriber chan []byte

// Hub is the centralized, concurrent real-time broadcast dispatcher.
type Hub struct {
	state              *RoomState
	repo               repository.QueueRepository
	renderOOB          ComponentRenderer
	renderTickOOB      ComponentRenderer
	register           chan subscriber
	unregister         chan subscriber
	broadcast          chan []byte
	clients            map[subscriber]struct{}
	hostMu             sync.Mutex
	activeHostID       string
	activeHostPriority bool
	activeHostSeen     time.Time
}

func NewHub(state *RoomState, repo repository.QueueRepository, renderOOB ComponentRenderer, renderTickOOB ...ComponentRenderer) *Hub {
	var tickRenderer ComponentRenderer
	if len(renderTickOOB) > 0 && renderTickOOB[0] != nil {
		tickRenderer = renderTickOOB[0]
	} else {
		tickRenderer = renderOOB
	}
	h := &Hub{
		state:         state,
		repo:          repo,
		renderOOB:     renderOOB,
		renderTickOOB: tickRenderer,
		register:      make(chan subscriber),
		unregister:    make(chan subscriber),
		broadcast:     make(chan []byte, 64),
		clients:       make(map[subscriber]struct{}),
	}
	go h.run()
	return h
}

func (h *Hub) State() *RoomState {
	return h.state
}

func (h *Hub) run() {
	for {
		select {
		case sub := <-h.register:
			h.clients[sub] = struct{}{}
		case sub := <-h.unregister:
			if _, ok := h.clients[sub]; ok {
				delete(h.clients, sub)
				close(sub)
			}
		case frame := <-h.broadcast:
			for sub := range h.clients {
				select {
				case sub <- frame:
				default:
					delete(h.clients, sub)
					close(sub)
				}
			}
		}
	}
}

// SyncFromRepository loads the daily queue from SQLite (claiming an active track if needed)
// and updates the in-memory RoomState.
func (h *Hub) SyncFromRepository(ctx context.Context) (RoomSnapshot, error) {
	if h.repo == nil {
		return h.state.Snapshot(), nil
	}

	entries, err := h.repo.ListDailyQueue(ctx)
	if err != nil {
		return h.state.Snapshot(), err
	}

	if len(entries) == 0 {
		if _, err := h.repo.AddRandomMusicsToQueue(ctx, 5, true); err == nil {
			entries, _ = h.repo.ListDailyQueue(ctx)
		}
	}

	hasPlaying := false
	for _, e := range entries {
		if e.Status == "playing" {
			hasPlaying = true
			break
		}
	}

	if !hasPlaying && len(entries) > 0 {
		if _, err := h.repo.CurrentOrClaimQueueItem(ctx); err == nil {
			entries, _ = h.repo.ListDailyQueue(ctx)
		}
	}

	h.state.SyncFromEntries(entries)
	return h.state.Snapshot(), nil
}

// renderSSEFrameWith renders the given templ OOB component into an in-memory bytes.Buffer
// and formats it as an SSE event stream payload.
func (h *Hub) renderSSEFrameWith(ctx context.Context, snapshot RoomSnapshot, renderer ComponentRenderer) ([]byte, error) {
	var htmlBuf bytes.Buffer
	if renderer != nil {
		comp := renderer(snapshot)
		if err := comp.Render(ctx, &htmlBuf); err != nil {
			return nil, err
		}
	}

	var frameBuf bytes.Buffer
	frameBuf.WriteString("event: room-sync\n")
	rawHTML := strings.TrimSpace(htmlBuf.String())
	for _, line := range strings.Split(rawHTML, "\n") {
		frameBuf.WriteString("data: ")
		frameBuf.WriteString(line)
		frameBuf.WriteByte('\n')
	}
	frameBuf.WriteByte('\n')

	// Also emit queue-updated event for any legacy listeners
	frameBuf.WriteString("event: queue-updated\ndata: {}\n\n")
	return frameBuf.Bytes(), nil
}

func (h *Hub) renderSSEFrame(ctx context.Context, snapshot RoomSnapshot) ([]byte, error) {
	return h.renderSSEFrameWith(ctx, snapshot, h.renderOOB)
}

// BroadcastState renders the current RoomState (including QueueOOB) into a bytes.Buffer via templ and broadcasts to all subscribers.
func (h *Hub) BroadcastState(ctx context.Context) error {
	snapshot := h.state.Snapshot()
	frame, err := h.renderSSEFrameWith(ctx, snapshot, h.renderOOB)
	if err != nil {
		return fmt.Errorf("hub render SSE frame: %w", err)
	}

	select {
	case h.broadcast <- frame:
	default:
	}
	return nil
}

// BroadcastPlaybackTick broadcasts only SyncMeta and Controls OOB fragments so #queue-container is untouched during time ticks.
func (h *Hub) BroadcastPlaybackTick(ctx context.Context) error {
	snapshot := h.state.Snapshot()
	frame, err := h.renderSSEFrameWith(ctx, snapshot, h.renderTickOOB)
	if err != nil {
		return fmt.Errorf("hub render playback tick SSE frame: %w", err)
	}

	select {
	case h.broadcast <- frame:
	default:
	}
	return nil
}

// SyncAndBroadcast reconciles RoomState with SQLite and dispatches the updated OOB fragments.
func (h *Hub) SyncAndBroadcast(ctx context.Context) {
	if _, err := h.SyncFromRepository(ctx); err != nil {
		log.Printf("error syncing room state from repository: %v", err)
	}
	if err := h.BroadcastState(ctx); err != nil {
		log.Printf("error broadcasting room state: %v", err)
	}
}

// EventsHandler serves the Server-Sent Events (SSE) stream for HTMX (hx-ext="sse") and clients.
func (h *Hub) EventsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming is not supported", http.StatusInternalServerError)
		return
	}

	sub := make(subscriber, 16)
	h.register <- sub
	defer func() {
		h.unregister <- sub
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Send initial synchronized room state immediately on connect
	snapshot := h.state.Snapshot()
	if snapshot.CurrentTrack == nil && snapshot.QueueTotal == 0 && h.repo != nil {
		snapshot, _ = h.SyncFromRepository(r.Context())
	}
	if initialFrame, err := h.renderSSEFrame(r.Context(), snapshot); err == nil {
		if _, err := w.Write(initialFrame); err != nil {
			return
		}
		flusher.Flush()
	}

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case frame, ok := <-sub:
			if !ok {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// HandlePlay resumes or starts playback in the shared room.
func (h *Hub) HandlePlay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := h.state.Play(); errors.Is(err, ErrEmptyQueue) && h.repo != nil {
		_, _ = h.SyncFromRepository(r.Context())
		_ = h.state.Play()
	}

	_ = h.BroadcastState(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

// HandlePause pauses playback in the shared room and preserves PauseOffset.
func (h *Hub) HandlePause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.state.Pause()
	_ = h.BroadcastState(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

// HandleSkip marks the current song played and advances the room to the next track.
func (h *Hub) HandleSkip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.advanceToNextTrack(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

const hostLeaseTTL = 6 * time.Second

func (h *Hub) verifyOrRefreshHostLease(clientID string) bool {
	h.hostMu.Lock()
	defer h.hostMu.Unlock()

	now := time.Now()
	expired := h.activeHostID == "" || now.Sub(h.activeHostSeen) >= hostLeaseTTL
	if clientID == "" {
		return expired
	}
	if expired || h.activeHostID == clientID {
		h.activeHostID = clientID
		h.activeHostSeen = now
		return true
	}
	return false
}

// HandleClaimHost ensures only a single loopback tab/window can hold the active Host role at a time.
func (h *Hub) HandleClaimHost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if !isLoopbackRequest(r) {
		_ = json.NewEncoder(w).Encode(map[string]bool{"isHost": false})
		return
	}

	var payload struct {
		ClientID  string `json:"clientId"`
		ForceHost bool   `json:"forceHost"`
		Release   bool   `json:"release"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.ClientID == "" {
		_ = json.NewEncoder(w).Encode(map[string]bool{"isHost": false})
		return
	}

	h.hostMu.Lock()
	now := time.Now()
	if payload.Release {
		if h.activeHostID == payload.ClientID {
			h.activeHostID = ""
			h.activeHostPriority = false
		}
		h.hostMu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]bool{"isHost": false})
		return
	}

	expired := h.activeHostID == "" || now.Sub(h.activeHostSeen) >= hostLeaseTTL
	isSameClient := h.activeHostID == payload.ClientID
	canPreempt := payload.ForceHost && !h.activeHostPriority

	isHost := false
	if expired || isSameClient || canPreempt {
		h.activeHostID = payload.ClientID
		if payload.ForceHost || (isSameClient && h.activeHostPriority) {
			h.activeHostPriority = true
		} else {
			h.activeHostPriority = false
		}
		h.activeHostSeen = now
		isHost = true
	}
	h.hostMu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]bool{"isHost": isHost})
}

// HandleHostSync receives authoritative playback telemetry (play/pause/seek/time/duration)
// directly from the Host media player and broadcasts it to all Remote followers.
func (h *Hub) HandleHostSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("X-Room-Role") != "host" || !isLoopbackRequest(r) {
		http.Error(w, "Forbidden: host loopback role required", http.StatusForbidden)
		return
	}
	if !h.verifyOrRefreshHostLease(r.Header.Get("X-Client-ID")) {
		http.Error(w, "Conflict: another window is already the active host", http.StatusConflict)
		return
	}

	var payload struct {
		TrackID     string  `json:"trackId"`
		Status      string  `json:"status"`
		Engine      string  `json:"engine"`
		CurrentTime float64 `json:"currentTime"`
		Duration    float64 `json:"duration"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid host sync payload", http.StatusBadRequest)
		return
	}

	status := StatusPaused
	if payload.Status == string(StatusPlaying) {
		status = StatusPlaying
	}

	prevSnap := h.state.Snapshot()
	currentDur := time.Duration(payload.CurrentTime * float64(time.Second))
	totalDur := time.Duration(payload.Duration * float64(time.Second))

	if h.state.UpdateFromHost(payload.TrackID, status, payload.Engine, currentDur, totalDur) {
		if prevSnap.Status != status {
			_ = h.BroadcastState(r.Context())
		} else {
			_ = h.BroadcastPlaybackTick(r.Context())
		}
	} else if payload.Engine != "" {
		_ = h.BroadcastPlaybackTick(r.Context())
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleTrackEnded is called exclusively by Host Mode when the media player fires onEnded (track_ended).
func (h *Hub) HandleTrackEnded(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("X-Room-Role") != "host" || !isLoopbackRequest(r) {
		http.Error(w, "Forbidden: only host can advance ended tracks", http.StatusForbidden)
		return
	}
	if !h.verifyOrRefreshHostLease(r.Header.Get("X-Client-ID")) {
		http.Error(w, "Conflict: another window is already the active host", http.StatusConflict)
		return
	}

	var payload struct {
		TrackID      string `json:"trackId"`
		QueueEntryID int64  `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)

	snap := h.state.Snapshot()
	if snap.CurrentTrack != nil {
		if payload.TrackID != "" && snap.CurrentTrack.VideoID != payload.TrackID {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if payload.QueueEntryID > 0 && snap.CurrentTrack.QueueEntryID != payload.QueueEntryID {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	h.advanceToNextTrack(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) advanceToNextTrack(ctx context.Context) {
	if h.repo != nil {
		snap := h.state.Snapshot()
		playedID := int64(0)
		if snap.CurrentTrack != nil && snap.CurrentTrack.QueueEntryID > 0 {
			playedID = snap.CurrentTrack.QueueEntryID
		} else if entries, err := h.repo.ListDailyQueue(ctx); err == nil {
			for _, e := range entries {
				if e.Status == "playing" {
					playedID = e.ID
					break
				}
			}
		}
		if playedID > 0 {
			_ = h.repo.MarkQueueItemPlayed(ctx, playedID)
		}
		if entry, err := h.repo.CurrentOrClaimQueueItem(ctx); err == nil && entry.ID > 0 {
			h.state.SetCurrentTrack(TrackFromQueueEntry(entry))
		}
		h.SyncAndBroadcast(ctx)
		return
	}

	h.state.Skip()
	_ = h.BroadcastState(ctx)
}

// HandlePlayItem selects a specific queue item by ID and starts playing it immediately across all clients.
func (h *Hub) HandlePlayItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := parseIDFromRequest(r)
	if id <= 0 {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	if h.repo != nil {
		entry, err := h.repo.PlayQueueItem(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Song not found in today's playlist", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "Error selecting song", http.StatusInternalServerError)
			return
		}
		h.state.SetCurrentTrack(TrackFromQueueEntry(entry))
		h.SyncAndBroadcast(r.Context())

		if strings.Contains(r.Header.Get("Accept"), "application/json") || r.Header.Get("HX-Request") == "" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(entry)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleRemoveQueueItem removes a track from the queue and broadcasts the updated queue OOB.
func (h *Hub) HandleRemoveQueueItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := parseIDFromRequest(r)
	if id <= 0 {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	snap := h.state.Snapshot()
	wasPlaying := snap.CurrentTrack != nil && snap.CurrentTrack.QueueEntryID == id

	if h.repo != nil {
		if err := h.repo.RemoveQueueItem(r.Context(), id); err != nil {
			http.Error(w, "Error removing item from queue", http.StatusInternalServerError)
			return
		}
		if wasPlaying {
			if nextEntry, err := h.repo.CurrentOrClaimQueueItem(r.Context()); err == nil && nextEntry.ID > 0 {
				h.state.SetCurrentTrack(TrackFromQueueEntry(nextEntry))
			}
		}
		h.SyncAndBroadcast(r.Context())
	} else {
		_ = h.state.RemoveFromQueue(strconv.FormatInt(id, 10))
		_ = h.BroadcastState(r.Context())
	}

	if r.Header.Get("HX-Request") != "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// HandleClearPlayed removes all already-played tracks from today's queue and broadcasts the updated queue OOB.
func (h *Hub) HandleClearPlayed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	removed := 0
	if h.repo != nil {
		var err error
		removed, err = h.repo.ClearPlayedQueueItems(r.Context())
		if err != nil {
			http.Error(w, "Error clearing played items from queue", http.StatusInternalServerError)
			return
		}
		if removed > 0 {
			h.SyncAndBroadcast(r.Context())
		}
	}

	if r.Header.Get("HX-Request") != "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "removed": removed})
}

// HandleMoveItemNext moves a queued track to play immediately after the currently playing track.
func (h *Hub) HandleMoveItemNext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := parseIDFromRequest(r)
	if id <= 0 {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	if h.repo != nil {
		if err := h.repo.MoveQueueItemNext(r.Context(), id); err != nil {
			http.Error(w, "Error moving item to play next", http.StatusInternalServerError)
			return
		}
		h.SyncAndBroadcast(r.Context())
	}

	if r.Header.Get("HX-Request") != "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func parseIDFromRequest(r *http.Request) int64 {
	if err := r.ParseForm(); err == nil {
		if formID := r.FormValue("id"); formID != "" {
			if parsed, err := strconv.ParseInt(formID, 10, 64); err == nil {
				return parsed
			}
		}
	}
	var payload struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
		return payload.ID
	}
	return 0
}

