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
	InQueue     bool       `json:"in_queue,omitempty"`
}

type Thumbnails struct {
	Default  Thumbnail  `json:"default"`
	Standard *Thumbnail `json:"standard,omitempty"`
	High     *Thumbnail `json:"high,omitempty"`
	Medium   *Thumbnail `json:"medium,omitempty"`
}

type Thumbnail struct {
	URL    string `json:"url"`
	Width  int64  `json:"width,omitempty"`
	Height int64  `json:"height,omitempty"`
}
