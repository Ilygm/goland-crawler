package internal

import (
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"crawler/helpers"

	"golang.org/x/net/html"
)

const (
	SafeSetBaseSize int = 11_000
	MaxUrlCrawl     int = 11_000
	CrawlerCount    int = 20
)

var (
	URLtoFilename  func(string) string
	totalCounter   uint32 = 0
	okCounter      uint32 = 0
	failCounter    uint32 = 0
	garbageCounter uint32 = 0
	client         *http.Client
	safeSet        *helpers.SafeSet
	queue          chan string
)

func init() {
	// Internal Data structures
	queue = make(chan string, 11_000)
	safeSet = helpers.NewSafeSet(11_000)
	// Internal helper functions
	base32Encoder := *base32.StdEncoding.WithPadding(base32.NoPadding)
	URLtoFilename = func(url string) string {
		hashed := sha256.Sum256([]byte(url))
		return base32Encoder.EncodeToString(hashed[:]) + ".html"
	}
	// Internal global objects
	client = &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
		Timeout: 5 * time.Second,
	}
}

func StartDownloader() {
	if _, err := os.Stat("./site"); os.IsNotExist(err) {
		os.Mkdir("./site", 0755)
	}
	queue <- "https://barbadpiano.com/"
	waiter := sync.WaitGroup{}
	for range CrawlerCount {
		time.Sleep(time.Duration(rand.IntN(4)) * time.Second)
		waiter.Go(func() {
		startLabel:
			for url := range queue {
				filename := "./site/" + URLtoFilename(url)
				if _, err := os.Stat(filename); os.IsNotExist(err) {
					resp, err := client.Get(url)
					startTIme := time.Now()
					if err != nil || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
						garbageCounter++
						continue
					} else {
						okCounter++
					}
					file, fileErr := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0755)
					if fileErr != nil {
						fmt.Println("Could not open file for storing", url)
						continue
					}
					data, _ := io.ReadAll(resp.Body)
					file.Write(data)
					file.Seek(0, 0)
					resp.Body.Close()

					// Extract document data from the HTML
					doc, extractErr := helpers.ExtractDocument(file, url)
					if extractErr != nil {
						fmt.Printf("Error extracting document from %s: %s\n", url, extractErr)
					} else {
						// Save extracted document as JSON file for later indexing
						if saveErr := helpers.SaveDocumentJSON(doc, filename); saveErr != nil {
							fmt.Printf("Error saving document JSON for %s: %s\n", url, saveErr)
						}
					}

					file.Seek(0, 0)
					StartParser(file)
					file.Close()
					time.Sleep(5 - time.Duration(time.Since(startTIme).Seconds()))
				} else {
					file, _ := os.OpenFile(filename, os.O_RDONLY, 0755)
					okCounter++
					StartParser(file)
					file.Close()
				}
				if len(queue) == 0 {
					time.Sleep(time.Second * 3)
					if len(queue) != 0 {
						goto startLabel
					}
					break
				}
			}
		})
	}
	fmt.Println("NOW WAITING")
	go func() {
		ticker := time.NewTicker(time.Second * 3)
		for range ticker.C {
			fmt.Printf("TOTAL PARSED URLS: %6d | TOTAL OK URLs: %6d | TOTAL FAILED URLS: %6d | TOTAL GARBAGE URLS: %6d \n", totalCounter, okCounter, failCounter, garbageCounter)
		}
	}()
	waiter.Wait()
	fmt.Println("ALL DONE")
}

func StartParser(file *os.File) {
	rootNode, err := html.Parse(file)
	if err != nil {
		fmt.Printf("BIG ERROR: %s\nFilename: %s\n", err.Error(), file.Name())
		file.Close()
		failCounter++
	}
	chainParser(rootNode)
}

func chainParser(node *html.Node) {
	if node.Type == html.ElementNode && node.Data == "a" {
		for _, attr := range node.Attr {
			if attr.Key == "href" {
				data := strings.Split(strings.TrimSpace(attr.Val), "#")[0]
				if strings.HasPrefix(data, "https://") && strings.Contains(data, "barbadpiano.com") {
					if ok := safeSet.AddIfNotExists(data); ok {
						totalCounter++
						queue <- data
					}
				}
			}
		}
	}
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		chainParser(c)
	}
}
