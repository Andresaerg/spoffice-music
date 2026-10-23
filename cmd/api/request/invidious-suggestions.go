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

// SearchSuggestions queries Invidious for search autocomplete suggestions (GET /api/v1/search/suggestions).
func SearchSuggestions(query string) ([]string, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return []string{}, nil
	}

	endpoint := strings.TrimRight(config.InvidiousHost, "/") + "/api/v1/search/suggestions?q=" + url.QueryEscape(q)

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating suggestions request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error executing suggestions request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("invidious suggestions returned status %d", resp.StatusCode)
	}

	var res models.InvidiousSuggestionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("error decoding suggestions response: %w", err)
	}

	return res.Suggestions, nil
}
