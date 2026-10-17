package models

type YTApiResponse struct {
	Items []Item `json:"items"`
}

type Item struct {
	Kind    string  `json:"kind"`
	ID      ID      `json:"id"`
	Snippet Snippet `json:"snippet"`
}

type ID struct {
	Kind    string `json:"kind"`
	VideoID string `json:"videoId"`
}

type Snippet struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Thumbnails  Thumbnails `json:"thumbnails"`
}

type Thumbnails struct {
	Default  Thumbnail  `json:"default"`
	Standard *Thumbnail `json:"standard"`
	High     *Thumbnail `json:"high"`
	Medium   *Thumbnail `json:"medium"`
}

type Thumbnail struct {
	URL    string `json:"url"`
	Width  int64  `json:"width"`
	Height int64  `json:"height"`
}
