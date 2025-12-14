package internal

import (
	"crawler/helpers"
	"crawler/models"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FixJSONFiles re-parses all HTML files and regenerates JSON files with proper encoding
// This is useful when encoding issues are fixed and you want to update existing JSON files
func FixJSONFiles(dataDir string) {
	log.Println("--- Fixing JSON files by re-parsing HTML with proper encoding ---")
	startTime := time.Now()

	files, err := os.ReadDir(dataDir)
	if err != nil {
		log.Fatalf("Error reading data directory: %s", err)
	}
	processed := 0
	errors := 0
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".html") {
			continue
		}

		filePath := filepath.Join(dataDir, file.Name())
		jsonPath := strings.TrimSuffix(filePath, ".html") + ".json"
		// Try to get original URL from existing JSON file
		var originalURL string
		if jsonData, err := os.ReadFile(jsonPath); err == nil {
			var existingDoc models.Document
			if err := json.Unmarshal(jsonData, &existingDoc); err == nil && existingDoc.URL != "" {
				originalURL = existingDoc.URL
			}
		}
		// If we couldn't get the URL from JSON, use filename as fallback
		if originalURL == "" {
			originalURL = file.Name()
		}

		// Open and re-parse the HTML file with proper encoding
		f, err := os.Open(filePath)
		if err != nil {
			log.Printf("Error opening %s: %s", file.Name(), err)
			errors++
			continue
		}
		// Extract document with proper encoding
		doc, err := helpers.ExtractDocument(f, originalURL)
		f.Close()
		if err != nil {
			log.Printf("Error extracting document from %s: %s", file.Name(), err)
			errors++
			continue
		}
		// Save the updated JSON file
		if err := helpers.SaveDocumentJSON(doc, filePath); err != nil {
			log.Printf("Error saving JSON for %s: %s", file.Name(), err)
			errors++
			continue
		}
		processed++
		if processed%100 == 0 {
			log.Printf("Processed %d files...", processed)
		}
	}
	log.Printf("Fixed %d JSON files in %s (errors: %d)", processed, time.Since(startTime), errors)
}
