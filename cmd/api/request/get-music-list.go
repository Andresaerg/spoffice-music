package request

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lmf3z/smlf/cmd/config"
	"github.com/lmf3z/smlf/cmd/models"
)

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// SearchInvidiousWithParams queries the Invidious search API endpoint (GET /api/v1/search) with full options.
func SearchInvidiousWithParams(p models.InvidiousSearchParams) ([]models.InvidiousSearchResultItem, error) {
	values := url.Values{}
	values.Set("q", p.Query)

	if p.Page > 0 {
		values.Set("page", strconv.Itoa(int(p.Page)))
	}
	if p.Sort != "" {
		values.Set("sort", p.Sort)
	}
	if p.Date != "" {
		values.Set("date", p.Date)
	}
	if p.Duration != "" {
		values.Set("duration", p.Duration)
	}
	if p.Type != "" {
		values.Set("type", p.Type)
	}
	if p.Features != "" {
		values.Set("features", p.Features)
	}
	if p.Region != "" {
		values.Set("region", p.Region)
	}

	endpoint := strings.TrimRight(config.InvidiousHost(), "/") + "/api/v1/search?" + values.Encode()

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating invidious search request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error executing invidious search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("invidious search returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var results []models.InvidiousSearchResultItem
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("error decoding invidious search response: %w", err)
	}

	return results, nil
}

// SearchInvidious queries the Invidious search API endpoint (GET /api/v1/search) for videos.
func SearchInvidious(query string) ([]models.InvidiousSearchResultItem, error) {
	return SearchInvidiousWithParams(models.InvidiousSearchParams{
		Query:  query,
		Type:   "video",
		Region: "US",
	})
}

// GetMusicList searches music using Invidious and converts results into YTSong models.
func GetMusicList(query string, path string) ([]models.YTSong, error) {
	items, err := SearchInvidious(query)
	if err != nil {
		return nil, err
	}

	songs := make([]models.YTSong, 0, len(items))
	for _, item := range items {
		if item.VideoID == "" || (item.Type != "" && item.Type != "video") {
			continue
		}

		thumbnails := mapThumbnails(item.VideoID)

		songs = append(songs, models.YTSong{
			Kind:        "youtube#video",
			VideoID:     item.VideoID,
			Title:       item.Title,
			Description: item.Description,
			Thumbnails:  thumbnails,
		})
	}

	return songs, nil
}

func mapThumbnails(videoID string) models.Thumbnails {
	hq := models.Thumbnail{URL: "https://i.ytimg.com/vi/" + videoID + "/hqdefault.jpg", Width: 480, Height: 360}
	sd := models.Thumbnail{URL: "https://i.ytimg.com/vi/" + videoID + "/sddefault.jpg", Width: 640, Height: 480}
	mq := models.Thumbnail{URL: "https://i.ytimg.com/vi/" + videoID + "/mqdefault.jpg", Width: 320, Height: 180}

	return models.Thumbnails{
		Default:  hq,
		High:     &hq,
		Standard: &sd,
		Medium:   &mq,
	}
}
