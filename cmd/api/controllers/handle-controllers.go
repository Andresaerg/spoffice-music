package controllers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lmf3z/smlf/cmd/api/repository"
	"github.com/lmf3z/smlf/cmd/api/request"
	"github.com/lmf3z/smlf/cmd/models"
	webtemplates "github.com/lmf3z/smlf/templates"
)

var templates = template.Must(template.ParseFS(webtemplates.Files, "*.html"))
var youtubeVideoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

type Handlers struct {
	queueRepository  repository.QueueRepository
	quotaRepository  repository.YouTubeQuotaRepository
	queueEventsMu    sync.Mutex
	queueSubscribers map[chan string]struct{}
}

func NewHandlers(queueRepository repository.QueueRepository, quotaRepository repository.YouTubeQuotaRepository) *Handlers {
	return &Handlers{
		queueRepository:  queueRepository,
		quotaRepository:  quotaRepository,
		queueSubscribers: make(map[chan string]struct{}),
	}
}

func (h *Handlers) NotifyQueueChanged() {
	h.notify("queue-updated")
}

func (h *Handlers) NotifyQuotaChanged() {
	h.notify("youtube-quota-updated")
}

func (h *Handlers) notify(event string) {
	h.queueEventsMu.Lock()
	defer h.queueEventsMu.Unlock()

	for subscriber := range h.queueSubscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (h *Handlers) subscribeQueueEvents() (chan string, func()) {
	subscriber := make(chan string, 1)
	h.queueEventsMu.Lock()
	h.queueSubscribers[subscriber] = struct{}{}
	h.queueEventsMu.Unlock()

	return subscriber, func() {
		h.queueEventsMu.Lock()
		delete(h.queueSubscribers, subscriber)
		h.queueEventsMu.Unlock()
	}
}

func (h *Handlers) QueueEventsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming is not supported", http.StatusInternalServerError)
		return
	}

	updates, unsubscribe := h.subscribeQueueEvents()
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	if _, err := fmt.Fprint(w, "event: queue-updated\ndata: {}\n\n"); err != nil {
		return
	}
	flusher.Flush()

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
		case event := <-updates:
			if _, err := fmt.Fprintf(w, "event: %s\ndata: {}\n\n", event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *Handlers) SearchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("query")
	if query == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}

	localSongs, err := h.queueRepository.SearchMusics(r.Context(), query)

	resp := make([]models.YTSong, 0, len(localSongs))

	if err == nil {
		for _, song := range localSongs {
			resp = append(resp, models.YTSong{
				VideoID:     song.VideoID,
				Title:       song.Title,
				Description: song.Description,
				Thumbnails:  models.Thumbnails{Default: models.Thumbnail{URL: song.Thumbnail}},
			})
		}
	}

	if err != nil || len(resp) == 0 {
		calls, allowed, quotaErr := h.quotaRepository.ReserveCall(r.Context())
		if quotaErr != nil {
			http.Error(w, "Error reserving YouTube API call", http.StatusInternalServerError)
			return
		}
		if !allowed {
			w.Header().Set("X-YouTube-Quota-Used", fmt.Sprintf("%d", calls))
			w.Header().Set("X-YouTube-Quota-Limit", fmt.Sprintf("%d", repository.DailyYouTubeCallLimit))
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprintf(w, `<li class="text-amber-700">Ya no hay más llamadas a YouTube por hoy. Inténtalo de nuevo mañana. (%d/%d)</li>`, calls, repository.DailyYouTubeCallLimit)
			return
		}
		h.NotifyQuotaChanged()

		resp, err = request.GetMusicList(query, "/search")

		if err != nil {
			http.Error(w, "Error fetching music list", http.StatusInternalServerError)
			return
		}

	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := templates.ExecuteTemplate(w, "item-list-result.html", resp); err != nil {
		http.Error(w, "Error rendering results", http.StatusInternalServerError)
	}
}

func (h *Handlers) YouTubeQuotaHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	calls, err := h.quotaRepository.GetCallCount(r.Context())
	if err != nil {
		http.Error(w, "Error fetching YouTube API call count", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(struct {
		Used      int `json:"used"`
		Limit     int `json:"limit"`
		Remaining int `json:"remaining"`
	}{
		Used:      calls,
		Limit:     repository.DailyYouTubeCallLimit,
		Remaining: repository.DailyYouTubeCallLimit - calls,
	}); err != nil {
		http.Error(w, "Error encoding YouTube API call count", http.StatusInternalServerError)
	}
}

func (h *Handlers) AddToPlaylistHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	queue := models.Queue{
		VideoID:     r.FormValue("video_id"),
		Title:       r.FormValue("title"),
		Description: r.FormValue("description"),
		Thumbnail:   r.FormValue("thumbnail"),
	}

	added, err := h.queueRepository.AddToQueue(r.Context(), queue)
	if err != nil {
		http.Error(w, "Error adding music to playlist", http.StatusInternalServerError)
		return
	}
	if added {
		h.NotifyQueueChanged()
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	message := "Canción agregada a la lista"
	status := http.StatusCreated
	toastClass := "fixed top-5 right-5 rounded bg-green-600 px-4 py-3 text-white shadow-lg"
	if !added {
		message = "La canción ya está en la lista de hoy"
		status = http.StatusOK
		toastClass = "fixed top-5 right-5 rounded bg-slate-700 px-4 py-3 text-white shadow-lg"
	}
	w.WriteHeader(status)

	fmt.Fprintf(w, `
        <div
            id="toast"
            hx-swap-oob="true"
            class="%s"
        >
            %s
        </div>
	`, toastClass, message)
}

func (h *Handlers) AddYouTubeLinkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&payload); err != nil {
		http.Error(w, "A valid YouTube video URL is required", http.StatusBadRequest)
		return
	}

	videoID, err := youtubeVideoIDFromLink(payload.URL)
	if err != nil {
		http.Error(w, "El enlace no es un video compatible de YouTube", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(payload.Title)
	description := "Agregado mediante enlace directo de YouTube"
	if title == "" {
		metadata, err := request.GetYouTubeMetadata(r.Context(), videoID)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			if encodeErr := json.NewEncoder(w).Encode(struct {
				NeedsTitle bool   `json:"needs_title"`
				Message    string `json:"message"`
			}{
				NeedsTitle: true,
				Message:    "No se pudo obtener el título de YouTube. Escribe el nombre del video para continuar.",
			}); encodeErr != nil {
				http.Error(w, "Error encoding metadata fallback", http.StatusInternalServerError)
			}
			return
		}
		title = metadata.Title
		if metadata.AuthorName != "" {
			description = "Canal: " + metadata.AuthorName + " · " + description
		}
	} else {
		if len([]rune(title)) > 200 {
			http.Error(w, "El título no puede superar los 200 caracteres", http.StatusBadRequest)
			return
		}
		description = "Título ingresado manualmente · " + description
	}

	added, err := h.queueRepository.AddToQueue(r.Context(), models.Queue{
		VideoID:     videoID,
		Title:       title,
		Description: description,
		Thumbnail:   "https://i.ytimg.com/vi/" + videoID + "/hqdefault.jpg",
	})
	if err != nil {
		http.Error(w, "Error adding YouTube video to playlist", http.StatusInternalServerError)
		return
	}
	h.NotifyQueueChanged()

	w.Header().Set("Content-Type", "application/json")
	message := "El video ya está en la lista de hoy"
	if added {
		message = "Enlace agregado a la lista"
		w.WriteHeader(http.StatusCreated)
	}
	if err := json.NewEncoder(w).Encode(struct {
		Added      bool   `json:"added"`
		Message    string `json:"message"`
		VideoID    string `json:"video_id"`
		YouTubeURL string `json:"youtube_url"`
	}{
		Added:      added,
		Message:    message,
		VideoID:    videoID,
		YouTubeURL: "https://www.youtube.com/watch?v=" + videoID,
	}); err != nil {
		http.Error(w, "Error encoding YouTube video result", http.StatusInternalServerError)
	}
}

func youtubeVideoIDFromLink(link string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(link))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Port() != "" {
		return "", errors.New("invalid YouTube URL")
	}

	host := strings.ToLower(parsed.Hostname())
	var videoID string
	switch host {
	case "youtu.be":
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(segments) == 1 {
			videoID = segments[0]
		}
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com":
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if parsed.Path == "/watch" || parsed.Path == "watch" {
			videoID = parsed.Query().Get("v")
		} else if len(segments) == 2 && (segments[0] == "shorts" || segments[0] == "embed" || segments[0] == "live") {
			videoID = segments[1]
		}
	}

	if !youtubeVideoIDPattern.MatchString(videoID) {
		return "", errors.New("invalid YouTube video ID")
	}
	return videoID, nil
}

func (h *Handlers) ListQueueHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	entries, err := h.queueRepository.ListDailyQueue(r.Context())
	if err != nil {
		http.Error(w, "Error fetching playlist", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(entries); err != nil {
		http.Error(w, "Error encoding playlist", http.StatusInternalServerError)
	}
}

func (h *Handlers) AddRandomMusicsToQueueHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	added, err := h.queueRepository.AddRandomMusicsToQueue(
		r.Context(),
		20,
		r.URL.Query().Get("if_empty") == "true",
	)
	if err != nil {
		http.Error(w, "Error adding random music to playlist", http.StatusInternalServerError)
		return
	}
	if added > 0 {
		h.NotifyQueueChanged()
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(struct {
		Added int `json:"added"`
	}{Added: added}); err != nil {
		http.Error(w, "Error encoding random music result", http.StatusInternalServerError)
	}
}

func (h *Handlers) PlayQueueItemHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.ID == 0 {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	entry, err := h.queueRepository.PlayQueueItem(r.Context(), payload.ID)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Song not found in today's playlist", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Error selecting song", http.StatusInternalServerError)
		return
	}
	h.NotifyQueueChanged()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(entry); err != nil {
		http.Error(w, "Error encoding selected song", http.StatusInternalServerError)
	}
}

func (h *Handlers) NextQueueHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	entry, err := h.queueRepository.CurrentOrClaimQueueItem(r.Context())
	if err != nil {
		http.Error(w, "Error fetching next song", http.StatusInternalServerError)
		return
	}
	h.NotifyQueueChanged()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

func (h *Handlers) FinishQueueHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.ID == 0 {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	if err := h.queueRepository.MarkQueueItemPlayed(r.Context(), payload.ID); err != nil {
		http.Error(w, "Error finishing song", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
