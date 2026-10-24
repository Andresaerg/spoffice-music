package request

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lmf3z/smlf/cmd/config"
	"github.com/lmf3z/smlf/cmd/models"
)

// GetHashtagMusic fetches videos tagged with a specific hashtag (e.g. "music") from Invidious.
func GetHashtagMusic(tag string) ([]models.YTSong, error) {
	tag = strings.TrimPrefix(strings.TrimSpace(tag), "#")
	if tag == "" {
		tag = "music"
	}

	endpoint := strings.TrimRight(config.InvidiousHost(), "/") + "/api/v1/hashtag/" + url.PathEscape(tag)

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating hashtag request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error executing hashtag request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hashtag returned status %d", resp.StatusCode)
	}

	var data models.InvidiousHashtagResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("error decoding hashtag response: %w", err)
	}

	songs := make([]models.YTSong, 0, len(data.Results))
	for _, item := range data.Results {
		if item.VideoID == "" || (item.Type != "" && item.Type != "video") {
			continue
		}

		songs = append(songs, models.YTSong{
			Kind:        "youtube#video",
			VideoID:     item.VideoID,
			Title:       item.Title,
			Description: item.Description,
			Thumbnails:  mapThumbnails(item.VideoID),
		})
	}

	return songs, nil
}
