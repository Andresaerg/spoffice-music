package models

// GET api/v1/stats
type InvidiousStats struct {
	Version  string   `json:"version"`
	Software Software `json:"software"`
}

type Software struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Branch  string `json:"branch"`
}
