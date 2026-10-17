package models

type Queue struct {
	VideoID     string `json:"video_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Thumbnail   string `json:"thumbnail"`
}

type QueueEntry struct {
	ID          int64  `json:"id"`
	Position    int    `json:"position"`
	VideoID     string `json:"videoId"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Thumbnail   string `json:"thumbnail"`
	Status      string `json:"status"`
}
