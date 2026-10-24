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
	"text/template"
	"time"

	"github.com/joho/godotenv"

	"github.com/lmf3z/smlf/cmd/api/controllers"
	"github.com/lmf3z/smlf/cmd/api/repository"
	"github.com/lmf3z/smlf/cmd/api/request"
	"github.com/lmf3z/smlf/cmd/config"
	"github.com/lmf3z/smlf/cmd/database"
	"github.com/lmf3z/smlf/cmd/models"
	"github.com/lmf3z/smlf/static"
	webtemplates "github.com/lmf3z/smlf/templates"
)

//go:embed yt-music-lite-rmbg.png
var faviconBytes []byte

type PageData struct {
	InvidiousVersion string
	SoftwareVersion  string
	Subtitle         string
	CurrentPage      string
	InvidiousHost    string
	Songs            []models.YTSong
	PreloadTag       string
	QueueTotal       int
	QueueCurrent     int
}

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
		inQueue[entry.VideoID] = true
	}
	for i := range result {
		result[i].InQueue = inQueue[result[i].VideoID]
	}
	return result
}

func getPageData(currentPage, subtitle string) PageData {
	stats := getInvidiousStats()
	return PageData{
		InvidiousVersion: cmp.Or(stats.Version, "Owned Host"),
		SoftwareVersion:  stats.Software.Version,
		Subtitle:         subtitle,
		CurrentPage:      currentPage,
		InvidiousHost:    config.InvidiousHost(),
	}
}

func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("Error loading .env file: %v", err)
	}

	db := database.Connect()
	defer db.Close()
	queueRepository := repository.NewSQLQueueRepository(db)
	handlers := controllers.NewHandlers(queueRepository)

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
	} else {
		handlers.NotifyQueueChanged()
	}
	go scheduleDailyQueueCleanup(queueRepository, handlers, caracas)

	temp := template.Must(template.ParseFS(webtemplates.Files, "*.html"))

	http.HandleFunc("/list", func(w http.ResponseWriter, r *http.Request) {
		pageData := getPageData("list", "Busca canciones y administra la cola de hoy")
		cachedSongs := getPreloadedHashtagSongs("rock")
		pageData.Songs = markSongsInQueue(r.Context(), queueRepository, cachedSongs)
		pageData.PreloadTag = "rock"
		if total, err := queueRepository.CountQueue(r.Context()); err == nil {
			pageData.QueueTotal = total
		}
		if curr, err := queueRepository.CurrentPlayingPosition(r.Context()); err == nil {
			pageData.QueueCurrent = curr
		}
		temp.ExecuteTemplate(w, "index.html", pageData)
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		pageData := getPageData("home", "Lista de reproducción · canciones de hoy")
		cachedSongs := getPreloadedHashtagSongs("rock")
		pageData.Songs = markSongsInQueue(r.Context(), queueRepository, cachedSongs)
		pageData.PreloadTag = "rock"
		if total, err := queueRepository.CountQueue(r.Context()); err == nil {
			pageData.QueueTotal = total
		}
		if curr, err := queueRepository.CurrentPlayingPosition(r.Context()); err == nil {
			pageData.QueueCurrent = curr
		}
		temp.ExecuteTemplate(w, "index.html", pageData)
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
	http.HandleFunc("/search", handlers.SearchHandler)
	http.HandleFunc("/search/suggestions", handlers.SearchSuggestionsHandler)
	http.HandleFunc("/add-to-queue", handlers.AddToPlaylistHandler)
	http.HandleFunc("/add-youtube-link", handlers.AddYouTubeLinkHandler)
	http.HandleFunc("/queue", handlers.ListQueueHandler)
	http.HandleFunc("/queue/random", handlers.AddRandomMusicsToQueueHandler)
	http.HandleFunc("/queue/events", handlers.QueueEventsHandler)
	http.HandleFunc("/queue/play", handlers.PlayQueueItemHandler)
	http.HandleFunc("/queue/next", handlers.NextQueueHandler)
	http.HandleFunc("/queue/finish", handlers.FinishQueueHandler)
	http.HandleFunc("/queue/delete", handlers.RemoveQueueItemHandler)
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
