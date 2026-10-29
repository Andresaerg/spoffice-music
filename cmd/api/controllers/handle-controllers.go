package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/lmf3z/smlf/cmd/api/repository"
	"github.com/lmf3z/smlf/cmd/api/request"
	"github.com/lmf3z/smlf/cmd/config"
	"github.com/lmf3z/smlf/cmd/models"
	"github.com/lmf3z/smlf/components"
	"github.com/lmf3z/smlf/internal/room"
)

var youtubeVideoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

type Handlers struct {
	queueRepository repository.QueueRepository
	hub             *room.Hub
}

func NewHandlers(queueRepository repository.QueueRepository, hub *room.Hub) *Handlers {
	return &Handlers{
		queueRepository: queueRepository,
		hub:             hub,
	}
}

func (h *Handlers) NotifyQueueChanged() {
	if h.hub != nil {
		h.hub.SyncAndBroadcast(context.Background())
	}
}

func (h *Handlers) RefreshQueueHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.hub != nil {
		h.hub.SyncAndBroadcast(r.Context())
	}
	w.WriteHeader(http.StatusNoContent)
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

	resp, err := request.GetMusicList(query, "/search")
	localSongs, localErr := h.queueRepository.SearchMusics(r.Context(), query)

	if err != nil || len(resp) == 0 {
		if localErr == nil && len(localSongs) > 0 {
			resp = make([]models.YTSong, 0, len(localSongs))
			for _, song := range localSongs {
				resp = append(resp, models.YTSong{
					VideoID:     song.VideoID,
					Title:       song.Title,
					Description: song.Description,
					Thumbnails:  models.Thumbnails{Default: models.Thumbnail{URL: song.Thumbnail}},
				})
			}
		} else if err != nil {
			http.Error(w, "Error fetching music list", http.StatusInternalServerError)
			return
		}
	} else if localErr == nil && len(localSongs) > 0 {
		seen := make(map[string]bool, len(resp))
		for _, s := range resp {
			seen[s.VideoID] = true
		}
		for _, song := range localSongs {
			if !seen[song.VideoID] {
				resp = append(resp, models.YTSong{
					VideoID:     song.VideoID,
					Title:       song.Title,
					Description: song.Description,
					Thumbnails:  models.Thumbnails{Default: models.Thumbnail{URL: song.Thumbnail}},
				})
			}
		}
	}

	entries, err := h.queueRepository.ListDailyQueue(r.Context())
	if err == nil && len(entries) > 0 {
		inQueue := make(map[string]bool, len(entries))
		for _, e := range entries {
			if e.Status != "played" {
				inQueue[e.VideoID] = true
			}
		}
		for i := range resp {
			resp[i].InQueue = inQueue[resp[i].VideoID]
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := components.SearchResultsList(resp).Render(r.Context(), w); err != nil {
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

	_ = components.AddedToQueueResponse(queue, toastClass, message, badgeText).Render(r.Context(), w)
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

func (h *Handlers) AddRandomMusicsToQueueHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	added, err := h.queueRepository.AddRandomMusicsToQueue(
		r.Context(),
		5,
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
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
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
	_ = json.NewEncoder(w).Encode(suggestions)
}

// InvidiousCheckHandler probes the configured Invidious embed URL so the frontend can
// automatically fall back from Invidious to YouTube if Invidious returns 500 or cannot stream the video.
func (h *Handlers) InvidiousCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	videoID := strings.TrimSpace(r.URL.Query().Get("video_id"))
	w.Header().Set("Content-Type", "application/json")
	if !youtubeVideoIDPattern.MatchString(videoID) {
		_ = json.NewEncoder(w).Encode(map[string]any{"playable": false, "status": http.StatusBadRequest})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	embedURL := strings.TrimRight(config.InvidiousHost(), "/") + "/embed/" + url.PathEscape(videoID) + "?autoplay=1&listen=0&local=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, embedURL, nil)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"playable": false, "status": 0})
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"playable": false, "status": 0})
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
	bodyStr := string(bodyBytes)

	playable := resp.StatusCode >= 200 && resp.StatusCode < 400 && strings.Contains(bodyStr, "<video")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"playable": playable,
		"status":   resp.StatusCode,
	})
}
