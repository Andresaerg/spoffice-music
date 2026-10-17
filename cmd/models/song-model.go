package models

type Song struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	CoverURL string `json:"cover_url"`
}

type YTSong struct {
	Kind        string     `json:"kind"`
	VideoID     string     `json:"videoId"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Thumbnails  Thumbnails `json:"thumbnails"`
}
