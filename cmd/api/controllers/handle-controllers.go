package controllers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
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
	queueEventsMu    sync.Mutex
	queueSubscribers map[chan string]struct{}
}

func NewHandlers(queueRepository repository.QueueRepository) *Handlers {
	return &Handlers{
		queueRepository:  queueRepository,
		queueSubscribers: make(map[chan string]struct{}),
	}
}

func (h *Handlers) NotifyQueueChanged() {
	h.notify("queue-updated")
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
		resp, err = request.GetMusicList(query, "/search")
		if err != nil {
			http.Error(w, "Error fetching music list", http.StatusInternalServerError)
			return
		}
	}

	entries, err := h.queueRepository.ListDailyQueue(r.Context())
	if err == nil && len(entries) > 0 {
		inQueue := make(map[string]bool, len(entries))
		for _, e := range entries {
			inQueue[e.VideoID] = true
		}
		for i := range resp {
			resp[i].InQueue = inQueue[resp[i].VideoID]
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := templates.ExecuteTemplate(w, "item-list-result.html", resp); err != nil {
		http.Error(w, "Error rendering results", http.StatusInternalServerError)
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

	isNext := r.FormValue("play_next") == "true" || r.URL.Query().Get("next") == "true"

	var added bool
	var err error
	if isNext {
		added, err = h.queueRepository.AddToQueueNext(r.Context(), queue)
	} else {
		added, err = h.queueRepository.AddToQueue(r.Context(), queue)
	}

	if err != nil {
		http.Error(w, "Error adding music to playlist", http.StatusInternalServerError)
		return
	}
	if added {
		h.NotifyQueueChanged()
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	message := "Canción agregada a la lista"
	if isNext {
		message = "Canción agregada para reproducir a continuación"
	}
	status := http.StatusCreated
	toastClass := "fixed top-5 right-5 z-[100] rounded bg-green-600 px-4 py-3 text-white shadow-xl cursor-pointer select-none flex items-center justify-between gap-3 transition-all duration-300"
	if !added {
		message = "La canción ya está en la lista de hoy"
		status = http.StatusOK
		toastClass = "fixed top-5 right-5 z-[100] rounded bg-slate-700 px-4 py-3 text-white shadow-xl cursor-pointer select-none flex items-center justify-between gap-3 transition-all duration-300"
	}
	w.WriteHeader(status)

	badgeText := "Agregada a la cola"
	if isNext {
		badgeText = "Siguiente en cola"
	}
	if !added {
		badgeText = "Ya en la cola"
	}

	videoIDAttr := html.EscapeString(queue.VideoID)
	titleAttr := html.EscapeString(queue.Title)
	descAttr := html.EscapeString(queue.Description)
	thumbAttr := html.EscapeString(queue.Thumbnail)

	fmt.Fprintf(w, `
        <div
            id="toast"
            hx-swap-oob="true"
            class="%s"
            onclick="this.classList.add('hidden')"
            title="Toca para cerrar"
        >
            <span>%s</span>
            <button type="button" class="text-xs opacity-75 hover:opacity-100 font-bold ml-2">✕</button>
        </div>
        <form class="flex flex-wrap items-center gap-2" data-video-id="%s">
            <input type="hidden" name="video_id" value="%s">
            <input type="hidden" name="title" value="%s">
            <input type="hidden" name="description" value="%s">
            <input type="hidden" name="thumbnail" value="%s">
            <button disabled type="button" class="cursor-default inline-flex items-center gap-1.5 rounded border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-xs font-semibold text-emerald-400 opacity-90">
                <svg class="size-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/></svg>
                <span>%s</span>
            </button>
        </form>
	`, toastClass, message, videoIDAttr, videoIDAttr, titleAttr, descAttr, thumbAttr, badgeText)
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
	h.NotifyQueueChanged()

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) RemoveQueueItemHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
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

	if err := h.queueRepository.RemoveQueueItem(r.Context(), payload.ID); err != nil {
		http.Error(w, "Error removing item from queue", http.StatusInternalServerError)
		return
	}
	h.NotifyQueueChanged()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (h *Handlers) ReorderQueueHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "ids are required", http.StatusBadRequest)
		return
	}

	if err := h.queueRepository.ReorderQueue(r.Context(), payload.IDs); err != nil {
		http.Error(w, "Error reordering queue", http.StatusInternalServerError)
		return
	}
	h.NotifyQueueChanged()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (h *Handlers) SearchSuggestionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("query")
	if query == "" {
		query = r.URL.Query().Get("q")
	}

	suggestions, err := request.SearchSuggestions(query)
	if err != nil {
		suggestions = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(suggestions)
}


