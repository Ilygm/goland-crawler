package models

type Document struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Body  string `json:"body"`
	H1    string `json:"h1"`
	H2    string `json:"h2"`
	H3    string `json:"h3"`
	H4    string `json:"h4"`
	H5    string `json:"h5"`
	H6    string `json:"h6"`
}
