package request

import (
	"encoding/json"
	"net/http"

	"github.com/lmf3z/smlf/cmd/config"
	"github.com/lmf3z/smlf/cmd/models"
)

// InvidiousStats makes a GET request to the invidious API to get the stats, used to check
// availability and test usage (api/v1/stats).
func InvidiousStats() (models.InvidiousStats, error) {
	resp, err := http.Get(config.InvidiousHost + "/api/v1/stats")
	if err != nil {
		return models.InvidiousStats{}, err
	}
	defer resp.Body.Close()

	var stats models.InvidiousStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return models.InvidiousStats{}, err
	}

	return stats, nil
}
