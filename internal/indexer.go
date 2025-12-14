package internal

import (
	"bytes"
	"context"
	"crawler/models"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
)

func CreatePersianIndex(es *elasticsearch.Client, indexName string) error {
	settings := map[string]any{
		"settings": map[string]any{
			"analysis": map[string]any{
				"char_filter": map[string]any{
					"zero_width_joiner": map[string]any{
						"type":     "mapping",
						"mappings": []string{"\u200C=>"},
					},
				},
				"filter": map[string]any{
					"persian_edge_ngram": map[string]any{
						"type":     "edge_ngram",
						"min_gram": 2,
						"max_gram": 15,
					},
					"persian_shingle": map[string]any{
						"type":             "shingle",
						"min_shingle_size": 2,
						"max_shingle_size": 3,
						"output_unigrams":  false,
					},
					"persian_reverse": map[string]any{
						"type": "reverse",
					},
				},
				"analyzer": map[string]any{
					"persian_index": map[string]any{
						"type":        "custom",
						"char_filter": []string{"html_strip", "zero_width_joiner"},
						"tokenizer":   "standard",
						"filter": []string{
							"lowercase",
							"arabic_normalization",
							"persian_normalization",
							"persian_stem",
						},
					},
					"persian_autocomplete": map[string]any{
						"type":        "custom",
						"char_filter": []string{"zero_width_joiner"},
						"tokenizer":   "standard",
						"filter": []string{
							"lowercase",
							"arabic_normalization",
							"persian_normalization",
							"persian_stem",
							"persian_edge_ngram",
						},
					},
					"persian_search": map[string]any{
						"type":        "custom",
						"char_filter": []string{"zero_width_joiner"},
						"tokenizer":   "standard",
						"filter": []string{
							"lowercase",
							"arabic_normalization",
							"persian_normalization",
							"persian_stem",
						},
					},
					"persian_suggestion": map[string]any{
						"type":        "custom",
						"char_filter": []string{"zero_width_joiner"},
						"tokenizer":   "standard",
						"filter": []string{
							"lowercase",
							"arabic_normalization",
							"persian_normalization",
							"persian_stem",
							"persian_shingle",
						},
					},
					"persian_reverse": map[string]any{
						"type":        "custom",
						"char_filter": []string{"zero_width_joiner"},
						"tokenizer":   "standard",
						"filter": []string{
							"lowercase",
							"arabic_normalization",
							"persian_normalization",
							"persian_stem",
							"persian_reverse",
						},
					},
				},
			},
		},
		"mappings": map[string]any{
			"properties": map[string]any{
				"title": map[string]any{
					"type":            "text",
					"analyzer":        "persian_index",
					"search_analyzer": "persian_search",
					"fields": map[string]any{
						"autocomplete": map[string]any{
							"type":            "text",
							"analyzer":        "persian_autocomplete",
							"search_analyzer": "persian_search",
						},
						"suggest": map[string]any{
							"type":     "completion",
							"analyzer": "persian_index",
						},
						"suggestion": map[string]any{
							"type":     "text",
							"analyzer": "persian_suggestion",
						},
						"reverse": map[string]any{
							"type":     "text",
							"analyzer": "persian_reverse",
						},
					},
				},
				"h1":   map[string]any{"type": "text", "analyzer": "persian_index"},
				"h2":   map[string]any{"type": "text", "analyzer": "persian_index"},
				"h3":   map[string]any{"type": "text", "analyzer": "persian_index"},
				"h4":   map[string]any{"type": "text", "analyzer": "persian_index"},
				"h5":   map[string]any{"type": "text", "analyzer": "persian_index"},
				"h6":   map[string]any{"type": "text", "analyzer": "persian_index"},
				"body": map[string]any{"type": "text", "analyzer": "persian_index"},
				"url":  map[string]any{"type": "keyword"},
			},
		},
	}

	body, _ := json.Marshal(settings)
	res, err := es.Indices.Create(
		indexName,
		es.Indices.Create.WithBody(bytes.NewReader(body)),
	)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("failed to create index: %s", res.String())
	}
	return nil
}

func StartIndexing(es *elasticsearch.Client, dataDir string) {
	log.Println("--- Starting Offline Phase: Indexing ---")
	startTime := time.Now()
	const indexName = "html-indexer"
	es.Indices.Delete([]string{indexName})
	err := CreatePersianIndex(es, indexName)
	if err != nil {
		log.Fatalf("Failed to create index: %v", err)
	}
	log.Println("Created index")

	files, err := os.ReadDir(dataDir)
	if err != nil {
		log.Fatalf("Error reading data directory: %s", err)
	}
	ctx := context.Background()

	var bulkReq bytes.Buffer
	batchSize := 50
	count := 0
	for _, file := range files {
		if filepath.Ext(file.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(dataDir, file.Name())

		var doc models.Document
		jsonData, err := os.ReadFile(filePath)
		if err != nil {
			log.Printf("Failed reading %s: %v", file.Name(), err)
			continue
		}

		if err := json.Unmarshal(jsonData, &doc); err != nil {
			log.Printf("Invalid JSON %s: %v", file.Name(), err)
			continue
		}

		data, err := json.Marshal(doc)
		if err != nil {
			log.Printf("Failed marshaling %s: %v", file.Name(), err)
			continue
		}

		// Add to bulk: index action + doc
		meta := fmt.Appendf(nil, `{"index":{"_index":"%s"}}%s`, indexName, "\n")
		bulkReq.Write(meta)
		bulkReq.Write(data)
		bulkReq.Write([]byte("\n"))
		count++

		if count >= batchSize {
			flushBulk(es, &bulkReq, ctx)
			bulkReq.Reset()
			count = 0
		}
	}
	log.Printf("Indexing completed in %s", time.Since(startTime))
}

var batchcount int

func flushBulk(es *elasticsearch.Client, bulkReq *bytes.Buffer, ctx context.Context) {
	res, err := es.Bulk(bytes.NewReader(bulkReq.Bytes()), es.Bulk.WithContext(ctx))
	if err != nil {
		log.Printf("Bulk error: %v", err)
		return
	}
	defer res.Body.Close()

	if res.IsError() {
		body, _ := io.ReadAll(res.Body)
		log.Printf("Bulk ES error: %s", body)
	} else {
		batchcount++
		log.Printf("Bulk %d batch indexed successfully \n", batchcount)
	}
}
