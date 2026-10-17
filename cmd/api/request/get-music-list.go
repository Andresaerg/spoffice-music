package request

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/lmf3z/smlf/cmd/models"
)

func GetMusicList(query string, path string) ([]models.YTSong, error) {

	apiKey := os.Getenv("YOUTUBE_API_KEY")
	baseURL := os.Getenv("YOUTUBE_API_URL")

	params := url.Values{}
	params.Set("part", "snippet")
	params.Set("maxResults", "20")
	params.Set("q", query)
	params.Set("type", "video")
	params.Set("key", apiKey)

	fullURL := baseURL + path + "?" + params.Encode()

	resp, err := http.Get(fullURL)

	if err != nil {
		fmt.Println("Error al realizar la solicitud:", err)
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var ytResponse models.YTApiResponse
	if err := json.Unmarshal(body, &ytResponse); err != nil {
		fmt.Println("Error al analizar la respuesta JSON:", err)
		return nil, err
	}

	var songs []models.YTSong

	for _, item := range ytResponse.Items {
		song := models.YTSong{
			Kind:        item.Kind,
			VideoID:     item.ID.VideoID,
			Title:       item.Snippet.Title,
			Description: item.Snippet.Description,
			Thumbnails:  item.Snippet.Thumbnails,
		}

		songs = append(songs, song)
	}

	return songs, nil

}
