package internal

import (
	"bytes"
	"context"
	"crawler/helpers"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
)

func SearchIndexHandler(
	es *elasticsearch.Client,
	w http.ResponseWriter,
	query string,
	queryMap map[string]any,
	page, pageSize int,
	isAutocomplete bool,
) {
	start := time.Now()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(queryMap); err != nil {
		http.Error(w, "Error encoding query", http.StatusInternalServerError)
		return
	}
	from := (page - 1) * pageSize
	res, err := es.Search(
		es.Search.WithContext(context.Background()),
		es.Search.WithIndex("html-indexer"),
		es.Search.WithBody(&buf),
		es.Search.WithTrackTotalHits(true),
		es.Search.WithSize(pageSize), // Added for pagination
		es.Search.WithFrom(from),     // Added for pagination
	)
	if err != nil {
		http.Error(w, "Search failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer res.Body.Close()

	if res.IsError() {
		body, _ := io.ReadAll(res.Body)
		http.Error(w, "ES error: "+string(body), res.StatusCode)
		return
	}

	var raw map[string]any
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		http.Error(w, "Failed to decode response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]any{
		"time_taken": time.Since(start).String(),
	}

	if isAutocomplete {
		// ---------- AUTOCOMPLETE ----------
		suggest, ok := raw["suggest"].(map[string]any)
		if !ok {
			http.Error(w, "No suggest in response", http.StatusInternalServerError)
			return
		}
		var results []map[string]string
		for _, suggester := range []string{"title-suggest", "h1-suggest"} {
			if sugg, ok := suggest[suggester]; ok {
				suggArr, ok := sugg.([]any)
				if !ok || len(suggArr) == 0 {
					continue
				}
				opts, ok := suggArr[0].(map[string]any)["options"].([]any)
				if !ok {
					continue
				}
				for _, o := range opts {
					optMap, ok := o.(map[string]any)
					if !ok {
						continue
					}
					text, ok := optMap["text"].(string)
					if ok {
						results = append(results, map[string]string{"title": text})
					}
				}
			}
		}
		response["results"] = results
		writeJSON(w, response)
		return
	}

	// ---------- SEARCH ----------
	hitsI, ok := raw["hits"]
	if !ok {
		http.Error(w, "No hits in response", http.StatusInternalServerError)
		return
	}
	hits, ok := hitsI.(map[string]any)
	if !ok {
		http.Error(w, "Invalid hits format", http.StatusInternalServerError)
		return
	}
	totalI, ok := hits["total"]
	if !ok {
		http.Error(w, "No total in hits", http.StatusInternalServerError)
		return
	}
	totalMap, ok := totalI.(map[string]any)
	if !ok {
		http.Error(w, "Invalid total format", http.StatusInternalServerError)
		return
	}
	totalVal, ok := totalMap["value"].(float64)
	if !ok {
		http.Error(w, "Invalid total value", http.StatusInternalServerError)
		return
	}
	total := int(totalVal)

	hitsArr, ok := hits["hits"].([]any)
	if !ok {
		http.Error(w, "Invalid hits array", http.StatusInternalServerError)
		return
	}
	var results []map[string]any
	for _, h := range hitsArr {
		hMap, ok := h.(map[string]any)
		if !ok {
			continue
		}
		src, ok := hMap["_source"].(map[string]any)
		if !ok {
			continue
		}
		results = append(results, map[string]any{
			"title": src["title"],
			"url":   src["url"],
		})
	}

	response["total_hits"] = total
	response["results"] = results
	writeJSON(w, response)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
	}
}

func PersianSearchQuery(query string) map[string]any {
	return map[string]any{
		"query": map[string]any{
			"multi_match": map[string]any{
				"query":     helpers.NormalizePersian(query),
				"type":      "best_fields",
				"operator":  "and",
				"fuzziness": "AUTO",
				"fields": []string{
					"title^6",
					"h1^5",
					"h2^4",
					"h3^3",
					"h4^2",
					"h5^1.5",
					"h6^1.2",
					"body",
				},
			},
		},
	}
}

func PersianAutocompleteSuggest(query string) map[string]any {
	return map[string]any{
		"query": map[string]any{
			"match_phrase_prefix": map[string]any{
				"title.autocomplete": map[string]any{
					"query":          helpers.NormalizePersian(query),
					"max_expansions": 50,
				},
			},
		},
		"size":    10,
		"_source": []string{"title", "url"},
	}
}

func PersianKeywordCorrection(query string) map[string]any {
	return map[string]any{
		"suggest": map[string]any{
			"text-suggest": map[string]any{
				"text": helpers.NormalizePersian(query),
				"phrase": map[string]any{
					"field":      "title.suggestion",
					"max_errors": 2,
					"confidence": 0.3,
					"direct_generator": []map[string]any{
						{
							"field":        "title",
							"suggest_mode": "always",
						},
						{
							"field":        "title.reverse",
							"suggest_mode": "always",
							"pre_filter":   "persian_reverse",
							"post_filter":  "persian_reverse",
						},
					},
				},
			},
		},
		"_source": false,
	}
}
