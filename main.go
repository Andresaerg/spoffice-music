//go:generate go tool templ generate
package main

import (
	"cmp"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/joho/godotenv"

	"github.com/lmf3z/smlf/cmd/api/controllers"
	"github.com/lmf3z/smlf/cmd/api/repository"
	"github.com/lmf3z/smlf/cmd/api/request"
	"github.com/lmf3z/smlf/cmd/config"
	"github.com/lmf3z/smlf/cmd/database"
	"github.com/lmf3z/smlf/cmd/models"
	"github.com/lmf3z/smlf/components"
	"github.com/lmf3z/smlf/internal/room"
	"github.com/lmf3z/smlf/static"
)

//go:embed yt-music-lite-rmbg.png
var faviconBytes []byte

var (
	cachedStatsMu   sync.Mutex
	cachedStats     models.InvidiousStats
	cachedStatsTime time.Time

	cachedHashtagMu    sync.Mutex
	cachedHashtagSongs []models.YTSong
	cachedHashtagTag   string
	cachedHashtagTime  time.Time
)

func getInvidiousStats() models.InvidiousStats {
	cachedStatsMu.Lock()
	defer cachedStatsMu.Unlock()

	if time.Since(cachedStatsTime) < 10*time.Minute && cachedStats.Version != "" {
		return cachedStats
	}

	stats, err := request.InvidiousStats()
	if err != nil {
		log.Printf("Error fetching invidious stats: %v", err)
		return cachedStats
	}

	cachedStats = stats
	cachedStatsTime = time.Now()
	return cachedStats
}

func getPreloadedHashtagSongs(tag string) []models.YTSong {
	cachedHashtagMu.Lock()
	defer cachedHashtagMu.Unlock()

	if time.Since(cachedHashtagTime) < 15*time.Minute && cachedHashtagTag == tag && len(cachedHashtagSongs) > 0 {
		return cachedHashtagSongs
	}

	songs, err := request.GetHashtagMusic(tag)
	if err != nil {
		log.Printf("Error fetching hashtag %s: %v", tag, err)
		return cachedHashtagSongs
	}

	cachedHashtagSongs = songs
	cachedHashtagTag = tag
	cachedHashtagTime = time.Now()
	return cachedHashtagSongs
}

func markSongsInQueue(ctx context.Context, repo repository.QueueRepository, songs []models.YTSong) []models.YTSong {
	if len(songs) == 0 {
		return songs
	}
	result := make([]models.YTSong, len(songs))
	copy(result, songs)

	entries, err := repo.ListDailyQueue(ctx)
	if err != nil || len(entries) == 0 {
		return result
	}
	inQueue := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.Status != "played" {
			inQueue[entry.VideoID] = true
		}
	}
	for i := range result {
		result[i].InQueue = inQueue[result[i].VideoID]
	}
	return result
}

func getPageViewModel(currentPage, subtitle string, roomSnap room.RoomSnapshot) components.PageViewModel {
	stats := getInvidiousStats()
	return components.PageViewModel{
		InvidiousVersion: cmp.Or(stats.Version, "Owned Host"),
		SoftwareVersion:  stats.Software.Version,
		Subtitle:         subtitle,
		CurrentPage:      currentPage,
		InvidiousHost:    config.InvidiousHost(),
		Room:             roomSnap,
	}
}

func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("Error loading .env file: %v", err)
	}

	db := database.Connect()
	defer db.Close()
	queueRepository := repository.NewSQLQueueRepository(db)

	roomState := room.NewRoomState()
	hub := room.NewHub(roomState, queueRepository, components.RoomBroadcastOOB, components.RoomPlaybackOOB)
	handlers := controllers.NewHandlers(queueRepository, hub)

	caracas, err := time.LoadLocation("America/Caracas")
	if err != nil {
		caracas = time.FixedZone("VET", -4*60*60)
	}
	now := time.Now().In(caracas)
	cleanupDate := now.AddDate(0, 0, -1)
	if now.Hour() >= 22 {
		cleanupDate = now
	}
	if err := queueRepository.CleanQueueThroughDate(context.Background(), cleanupDate.Format("2006-01-02")); err != nil {
		log.Printf("Error cleaning expired playlist: %v", err)
	}
	if _, err := hub.SyncFromRepository(context.Background()); err != nil {
		log.Printf("Error initializing room state from repository: %v", err)
	}
	go scheduleDailyQueueCleanup(queueRepository, handlers, caracas)

	renderIndex := func(w http.ResponseWriter, r *http.Request, pageName, subtitle string) {
		roomSnap := roomState.Snapshot()
		if roomSnap.CurrentTrack == nil && roomSnap.QueueTotal == 0 {
			roomSnap, _ = hub.SyncFromRepository(r.Context())
		}
		vm := getPageViewModel(pageName, subtitle, roomSnap)
		cachedSongs := getPreloadedHashtagSongs("rock")
		preloadTag := "rock"
		if len(cachedSongs) == 0 {
			if savedSongs, err := queueRepository.ListSavedMusics(r.Context(), 12); err == nil && len(savedSongs) > 0 {
				cachedSongs = savedSongs
				preloadTag = "biblioteca"
			}
		}
		vm.Songs = markSongsInQueue(r.Context(), queueRepository, cachedSongs)
		vm.PreloadTag = preloadTag
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := components.IndexPage(vm).Render(r.Context(), w); err != nil {
			log.Printf("Error rendering index page: %v", err)
		}
	}

	http.HandleFunc("/list", func(w http.ResponseWriter, r *http.Request) {
		renderIndex(w, r, "list", "Busca canciones y administra la cola de hoy")
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		renderIndex(w, r, "home", "Lista de reproducción · canciones de hoy")
	})

	var staticFS http.FileSystem
	if _, err := os.Stat("static"); err == nil {
		staticFS = http.Dir("static")
	} else {
		staticFS = http.FS(static.Files)
	}
	http.Handle("/static/", http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.FileServer(staticFS).ServeHTTP(w, r)
	})))
	faviconHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(faviconBytes)
	}
	http.HandleFunc("/yt-music-lite-rmbg.png", faviconHandler)
	http.HandleFunc("/favicon.ico", faviconHandler)

	// Search & Queue Addition Endpoints
	http.HandleFunc("/search", handlers.SearchHandler)
	http.HandleFunc("/search/suggestions", handlers.SearchSuggestionsHandler)
	http.HandleFunc("/add-to-queue", handlers.AddToPlaylistHandler)
	http.HandleFunc("/add-youtube-link", handlers.AddYouTubeLinkHandler)

	// Synchronized Room Endpoints (SSE + HTMX OOB + Alpine.js Bridge)
	http.HandleFunc("/room/events", hub.EventsHandler)
	http.HandleFunc("/room/play", hub.HandlePlay)
	http.HandleFunc("/room/pause", hub.HandlePause)
	http.HandleFunc("/room/skip", hub.HandleSkip)
	http.HandleFunc("/room/play-item", hub.HandlePlayItem)
	http.HandleFunc("/room/claim-host", hub.HandleClaimHost)
	http.HandleFunc("/room/invidious-check", handlers.InvidiousCheckHandler)
	http.HandleFunc("/room/host-sync", hub.HandleHostSync)
	http.HandleFunc("/room/track-ended", hub.HandleTrackEnded)
	http.HandleFunc("/room/queue/remove", hub.HandleRemoveQueueItem)
	http.HandleFunc("/room/queue/clear-played", hub.HandleClearPlayed)
	http.HandleFunc("/room/queue/move-next", hub.HandleMoveItemNext)

	// Queue Action Endpoints
	http.HandleFunc("/queue/refresh", handlers.RefreshQueueHandler)
	http.HandleFunc("/queue/random", handlers.AddRandomMusicsToQueueHandler)
	http.HandleFunc("/queue/reorder", handlers.ReorderQueueHandler)

	fmt.Println("starting server en http://0.0.0.0:8000")

	http.ListenAndServe(":8000", nil)
}

func scheduleDailyQueueCleanup(queueRepository repository.QueueRepository, handlers *controllers.Handlers, location *time.Location) {
	for {
		now := time.Now().In(location)
		nextRun := time.Date(now.Year(), now.Month(), now.Day(), 22, 0, 0, 0, location)
		if !nextRun.After(now) {
			nextRun = nextRun.AddDate(0, 0, 1)
		}

		timer := time.NewTimer(time.Until(nextRun))
		<-timer.C

		cleanupDate := time.Now().In(location).Format("2006-01-02")
		if err := queueRepository.CleanQueueThroughDate(context.Background(), cleanupDate); err != nil {
			log.Printf("Error cleaning daily playlist: %v", err)
		} else {
			handlers.NotifyQueueChanged()
		}
	}
}
