package request

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type YouTubeMetadata struct {
	Title      string `json:"title"`
	AuthorName string `json:"author_name"`
}

func GetYouTubeMetadata(ctx context.Context, videoID string) (YouTubeMetadata, error) {
	return fetchYouTubeMetadata(ctx, videoID, &http.Client{Timeout: 5 * time.Second})
}

func fetchYouTubeMetadata(ctx context.Context, videoID string, client *http.Client) (YouTubeMetadata, error) {
	videoURL := "https://www.youtube.com/watch?v=" + url.QueryEscape(videoID)
	endpoint := "https://www.youtube.com/oembed?url=" + url.QueryEscape(videoURL) + "&format=json"

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return YouTubeMetadata{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return YouTubeMetadata{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return YouTubeMetadata{}, fmt.Errorf("YouTube oEmbed returned %s", resp.Status)
	}

	var metadata YouTubeMetadata
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&metadata); err != nil {
		return YouTubeMetadata{}, err
	}
	if strings.TrimSpace(metadata.Title) == "" {
		return YouTubeMetadata{}, errors.New("YouTube oEmbed returned no title")
	}
	metadata.Title = strings.TrimSpace(metadata.Title)
	metadata.AuthorName = strings.TrimSpace(metadata.AuthorName)
	return metadata, nil
}
