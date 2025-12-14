package helpers

import (
	"bytes"
	"crawler/models"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var escapeChars = regexp.MustCompile(`[\s\p{Zs}]+`)

func NormalizePersian(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for _, r := range s {
		switch r {
		case 'ي':
			r = 'ی'
		case 'ك':
			r = 'ک'
		case '\u200c', '\u200f', '\u202a', '\u202b':
			r = ' '
		case '\u00ac':
			r = ' '
		}
		b.WriteRune(r)
	}

	return escapeChars.ReplaceAllString(strings.TrimSpace(b.String()), " ")
}

func extractText(n *html.Node) string {
	var buf strings.Builder

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			buf.WriteString(node.Data)
			buf.WriteString(" ")
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(n)
	return strings.TrimSpace(buf.String())
}

// extractDocument extracts document data (Title, Body, H1-H6, URL) from an HTML file
func ExtractDocument(file *os.File, url string) (models.Document, error) {
	file.Seek(0, 0)

	raw, err := io.ReadAll(file)
	if err != nil {
		return models.Document{}, err
	}

	root, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return models.Document{}, err
	}

	var (
		title                  string
		body                   strings.Builder
		h1, h2, h3, h4, h5, h6 []string
	)

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript":
				return
			case "title":
				if n.FirstChild != nil {
					title = strings.TrimSpace(n.FirstChild.Data)
				}
			case "h1":
				h1 = append(h1, extractText(n))
			case "h2":
				h2 = append(h2, extractText(n))
			case "h3":
				h3 = append(h3, extractText(n))
			case "h4":
				h4 = append(h4, extractText(n))
			case "h5":
				h5 = append(h5, extractText(n))
			case "h6":
				h6 = append(h6, extractText(n))
			}
		}

		if n.Type == html.TextNode {
			body.WriteString(n.Data)
			body.WriteRune(' ')
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(root)

	return models.Document{
		URL:   url,
		Title: NormalizePersian(title),
		Body:  NormalizePersian(body.String()),
		H1:    NormalizePersian(strings.Join(h1, " ")),
		H2:    NormalizePersian(strings.Join(h2, " ")),
		H3:    NormalizePersian(strings.Join(h3, " ")),
		H4:    NormalizePersian(strings.Join(h4, " ")),
		H5:    NormalizePersian(strings.Join(h5, " ")),
		H6:    NormalizePersian(strings.Join(h6, " ")),
	}, nil
}

// saveDocumentJSON saves the extracted document as a JSON file alongside the HTML file
func SaveDocumentJSON(doc models.Document, htmlFilePath string) error {
	// Create JSON filename by replacing .html with .json
	jsonFilePath := strings.TrimSuffix(htmlFilePath, ".html") + ".json"

	// Marshal document to JSON with indentation for readability
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}

	// Write to file
	return os.WriteFile(jsonFilePath, data, 0644)
}
