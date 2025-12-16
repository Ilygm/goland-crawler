package internal

import (
	"bytes"
	"context"
	"crawler/helpers"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
)

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

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
		es.Search.WithSize(pageSize),
		es.Search.WithFrom(from),
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
		hitsArr, ok := hits["hits"].([]any)
		if !ok {
			http.Error(w, "Invalid hits array", http.StatusInternalServerError)
			return
		}
		var results []map[string]any
		queryTokens := strings.Fields(query)
		normQueryTokens := make([]string, len(queryTokens))
		for i, t := range queryTokens {
			normQueryTokens[i] = helpers.NormalizePersian(strings.ToLower(t))
		}
		var lastPrefix string
		var prefixTokens []string
		if len(queryTokens) > 0 {
			lastPrefix = queryTokens[len(queryTokens)-1]
			prefixTokens = queryTokens[:len(queryTokens)-1]
			normLastPrefix := normQueryTokens[len(normQueryTokens)-1]
			normPrefixTokens := normQueryTokens[:len(normQueryTokens)-1]
			for _, h := range hitsArr {
				hMap, ok := h.(map[string]any)
				if !ok {
					continue
				}
				src, ok := hMap["_source"].(map[string]any)
				if !ok {
					continue
				}
				title, ok := src["title"].(string)
				if !ok {
					continue
				}
				titleTokens := strings.Fields(title)
				normTitleTokens := make([]string, len(titleTokens))
				for i, t := range titleTokens {
					normTitleTokens[i] = helpers.NormalizePersian(strings.ToLower(t))
				}
				if len(titleTokens) <= len(prefixTokens) {
					continue
				}
				if !sliceEqual(normPrefixTokens, normTitleTokens[:len(normPrefixTokens)]) {
					continue
				}
				completed := titleTokens[len(prefixTokens)]
				normCompleted := normTitleTokens[len(normPrefixTokens)]
				if !strings.HasPrefix(normCompleted, normLastPrefix) {
					continue
				}
				prefixRunes := []rune(lastPrefix)
				completedRunes := []rune(completed)
				suffixRunes := completedRunes[len(prefixRunes):]
				suffix := string(suffixRunes)
				results = append(results, map[string]any{
					"title":  title,
					"url":    src["url"],
					"suffix": suffix,
				})
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
		result := map[string]any{
			"title": src["title"],
			"url":   src["url"],
		}
		if score, ok := hMap["_score"].(float64); ok {
			result["score"] = score
		}
		results = append(results, result)
	}

	response["total_hits"] = total
	response["results"] = results

	// Run correction suggest
	var suggestBuf bytes.Buffer
	suggestMap := PersianKeywordCorrection(query)
	if err := json.NewEncoder(&suggestBuf).Encode(suggestMap); err != nil {
		http.Error(w, "Error encoding suggest", http.StatusInternalServerError)
		return
	}
	suggestRes, err := es.Search(
		es.Search.WithContext(context.Background()),
		es.Search.WithIndex("html-indexer"),
		es.Search.WithBody(&suggestBuf),
	)
	if err != nil {
		// Silent fail for suggest
		writeJSON(w, response)
		return
	}
	defer suggestRes.Body.Close()
	if suggestRes.IsError() {
		writeJSON(w, response)
		return
	}
	var rawSuggest map[string]any
	if err := json.NewDecoder(suggestRes.Body).Decode(&rawSuggest); err != nil {
		writeJSON(w, response)
		return
	}
	suggest, ok := rawSuggest["suggest"].(map[string]any)
	if !ok {
		writeJSON(w, response)
		return
	}
	textSuggest, ok := suggest["text-suggest"].([]any)
	if !ok || len(textSuggest) == 0 {
		writeJSON(w, response)
		return
	}
	options, ok := textSuggest[0].(map[string]any)["options"].([]any)
	if !ok || len(options) == 0 {
		writeJSON(w, response)
		return
	}
	var suggestions []string
	for _, opt := range options {
		optMap, ok := opt.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := optMap["text"].(string); ok && text != helpers.NormalizePersian(query) {
			suggestions = append(suggestions, text)
		}
	}
	if len(suggestions) > 0 {
		response["problem"] = "Did you mean:"
		response["suggestions"] = suggestions
	}

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
					"body^0.8",
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
					"field": "title.suggestion",
					// Be a bit more aggressive so that typos like گیزار -> گیتار are suggested
					"max_errors": 3,
					"confidence": 0.0,
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

// CorrectionOnlyHandler runs only the phrase suggester and returns suggestions
// without doing a full search. Useful for debugging and for a dedicated /correction API.
func CorrectionOnlyHandler(
	es *elasticsearch.Client,
	w http.ResponseWriter,
	query string,
) {
	start := time.Now()

	var buf bytes.Buffer
	suggestMap := PersianKeywordCorrection(query)
	if err := json.NewEncoder(&buf).Encode(suggestMap); err != nil {
		http.Error(w, "Error encoding suggest", http.StatusInternalServerError)
		return
	}

	res, err := es.Search(
		es.Search.WithContext(context.Background()),
		es.Search.WithIndex("html-indexer"),
		es.Search.WithBody(&buf),
	)
	if err != nil {
		http.Error(w, "Correction search failed: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "Failed to decode correction response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]any{
		"time_taken": time.Since(start).String(),
	}

	suggest, ok := raw["suggest"].(map[string]any)
	if !ok {
		writeJSON(w, response)
		return
	}
	textSuggest, ok := suggest["text-suggest"].([]any)
	if !ok || len(textSuggest) == 0 {
		writeJSON(w, response)
		return
	}
	options, ok := textSuggest[0].(map[string]any)["options"].([]any)
	if !ok || len(options) == 0 {
		writeJSON(w, response)
		return
	}
	var suggestions []string
	for _, opt := range options {
		optMap, ok := opt.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := optMap["text"].(string); ok && text != helpers.NormalizePersian(query) {
			suggestions = append(suggestions, text)
		}
	}
	if len(suggestions) > 0 {
		response["problem"] = "Did you mean:"
		response["suggestions"] = suggestions
	}

	writeJSON(w, response)
}
