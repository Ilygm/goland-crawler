package main

import (
	"bytes"
	"context"
	"crawler/internal"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/joho/godotenv"
)

func main() {
	modeArg := flag.String("mode", "server", "Crawler mode that the program should run in")
	testIndex := flag.Bool("test", false, "Test indexes but compile time values")
	flag.Parse()
	godotenv.Load(".env")

	// 1. Initialize ES Client with Configuration
	// Elasticsearch is configured with SSL/TLS and requires authentication
	// Get credentials from environment variables or use defaults
	var es *elasticsearch.Client
	if *modeArg == "index" || *modeArg == "server" {
		esUser := os.Getenv("ELASTIC_USER")
		if esUser == "" {
			esUser = "elastic" // Default username
		}
		esPassword := os.Getenv("ELASTIC_PASSWORD")
		if esPassword == "" {
			log.Fatal("ELASTIC_PASSWORD environment variable is required. Set it to the password from your .env file or docker-compose setup.")
		}
		cfg := elasticsearch.Config{
			Addresses:      []string{"https://localhost:9200"},
			PoolCompressor: true,
			Username:       esUser,
			Password:       esPassword,
			// Skip certificate verification for localhost (use CA cert in production)
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, // Only for localhost development
				},
			},
		}
		var err error
		es, err = elasticsearch.NewClient(cfg)
		if err != nil {
			log.Fatalf("Error creating the Elasticsearch client: %s", err)
		}
		// Test the connection to Elasticsearch
		res, err := es.Info()
		if err != nil {
			log.Fatalf("Error getting response from Elasticsearch: %s", err)
		}
		defer res.Body.Close()
		if res.IsError() {
			log.Fatalf("Elasticsearch client connection failed: %s", res.String())
		} else {
			log.Println("Successfully connected to Elasticsearch.")
		}
	}

	switch *modeArg {
	case "crawl":
		internal.StartDownloader()
	case "fix":
		// Fix mode: Re-parse all HTML files and regenerate JSON files with proper encoding
		internal.FixJSONFiles("./site")
	case "index":
		internal.StartIndexing(es, "./site")
	case "server":
		if *testIndex {
			testQuerySearch(es, internal.PersianKeywordCorrection("گیتار"))
			testQuerySearch(es, internal.PersianSearchQuery("گیت"))
		} else {
			validate_query := func(w http.ResponseWriter, r *http.Request) (int, int, string) {
				query := r.URL.Query().Get("q")
				pageStr := r.URL.Query().Get("page")
				sizeStr := r.URL.Query().Get("pageSize")
				page := 1
				pageSize := 10
				if pageStr != "" {
					var err error
					page, err = strconv.Atoi(pageStr)
					if err != nil || page < 1 {
						page = 1
					}
				}
				if sizeStr != "" {
					var err error
					pageSize, err = strconv.Atoi(sizeStr)
					if err != nil || page < 1 {
						page = 1
					}
				}
				if query == "" {
					http.Error(w, "Query parameter 'q' is missing", http.StatusBadRequest)
					return 1, 1, ""
				}
				return page, pageSize, query
			}
			http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				data, err := os.ReadFile("./internal/ui.html")
				if err == nil {
					w.Header().Set("Content-Type", "text/html")
					w.Write(data)
				} else {
					log.Println("Failed to load html", err)
				}
			})
			http.HandleFunc("/correction", func(w http.ResponseWriter, r *http.Request) {
				page, size, query := validate_query(w, r)
				if query != "" {
					internal.SearchIndexHandler(es, w, query, internal.PersianKeywordCorrection(query), page, size, false)
				}
			})
			http.HandleFunc("/autocomplete", func(w http.ResponseWriter, r *http.Request) {
				page, size, query := validate_query(w, r)
				if query != "" {
					internal.SearchIndexHandler(es, w, query, internal.PersianAutocompleteSuggest(query), page, size, true)
				}
			})
			log.Fatal(http.ListenAndServe(":8080", nil))
		}
	default:
		fmt.Println("Invalid mode. Use 'crawl', 'fix', 'index', or 'server'.")
	}
}

func testQuerySearch(es *elasticsearch.Client, query map[string]any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(query); err != nil {
		log.Fatalf("Error encoding query: %s", err)
	}
	res, err := es.Search(
		es.Search.WithContext(context.Background()),
		es.Search.WithIndex("_all"),
		es.Search.WithBody(&buf),
		es.Search.WithTrackTotalHits(true),
	)
	if err != nil {
		log.Fatalf("Search failed: %s", err)
	}
	defer res.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		log.Fatalf("Failed to decode response: %s", err)
	}

	hits := result["hits"].(map[string]any)["hits"].([]any)
	fmt.Printf("Query: %s | Found %d results\n", query, len(hits))
	for _, hit := range hits {
		source := hit.(map[string]any)["_source"].(map[string]any)
		fmt.Println("Title:", source["title"], "| URL:", source["url"])
	}
}
