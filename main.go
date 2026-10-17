package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"text/template"
	"time"

	"github.com/joho/godotenv"

	"github.com/lmf3z/smlf/cmd/api/controllers"
	"github.com/lmf3z/smlf/cmd/api/repository"
	"github.com/lmf3z/smlf/cmd/database"
	webtemplates "github.com/lmf3z/smlf/templates"
)

func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("Error loading .env file: %v", err)
	}

	db := database.Connect()
	defer db.Close()
	queueRepository := repository.NewSQLQueueRepository(db)
	quotaRepository := repository.NewSQLYouTubeQuotaRepository(db)
	handlers := controllers.NewHandlers(queueRepository, quotaRepository)

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
		temp.ExecuteTemplate(w, "list-results.html", nil)
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		temp.ExecuteTemplate(w, "index.html", nil)
	})
	http.HandleFunc("/search", handlers.SearchHandler)
	http.HandleFunc("/youtube-quota", handlers.YouTubeQuotaHandler)
	http.HandleFunc("/add-to-queue", handlers.AddToPlaylistHandler)
	http.HandleFunc("/add-youtube-link", handlers.AddYouTubeLinkHandler)
	http.HandleFunc("/queue", handlers.ListQueueHandler)
	http.HandleFunc("/queue/random", handlers.AddRandomMusicsToQueueHandler)
	http.HandleFunc("/queue/events", handlers.QueueEventsHandler)
	http.HandleFunc("/queue/play", handlers.PlayQueueItemHandler)
	http.HandleFunc("/queue/next", handlers.NextQueueHandler)
	http.HandleFunc("/queue/finish", handlers.FinishQueueHandler)

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
