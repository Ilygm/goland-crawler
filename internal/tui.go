package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/paginator"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/text/unicode/bidi"
)

const (
	apiBase      = "http://localhost:8080"
	autoComplete = "/autocomplete?q="
	search       = "/search?q="
	pageSize     = 10 // Matches your server
)

var (
	inputStyle    = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Width(50).Align(lipgloss.Right)
	listStyle     = lipgloss.NewStyle().Margin(1, 0).Align(lipgloss.Right)
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("red"))
	suggestStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("yellow"))
	noResultsMsg  = "No results found."
	problemPrefix = "Problem: "
)

type state int

const (
	stateInput state = iota
	stateResults
)

type model struct {
	state         state
	input         textinput.Model
	autoList      list.Model // Autocomplete suggestions
	resultsList   list.Model // Search results
	paginator     paginator.Model
	query         string
	page          int
	totalPages    int
	totalHits     int
	problem       string
	suggestions   []string // For typo corrections
	originalItems []list.Item
	err           error
}

type searchResult struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	Suffix string `json:"suffix,omitempty"` // For autocomplete
}

type apiResponse struct {
	TotalHits int              `json:"total_hits"`
	Results   []searchResult   `json:"results"`
	Problem   string           `json:"problem"`
	Sugg      []map[string]any `json:"suggestions"` // e.g., [{"corrected_query": "..."}]
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Enter search query..."
	ti.Focus()
	ti.Width = 50
	p := paginator.New()
	p.Type = paginator.Dots
	p.PerPage = pageSize
	return model{
		input:       ti,
		autoList:    list.New([]list.Item{}, list.NewDefaultDelegate(), 50, 10),
		resultsList: list.New([]list.Item{}, list.NewDefaultDelegate(), 50, 20),
		paginator:   p,
		page:        1,
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.state {
		case stateInput:
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "enter":
				m.query = m.input.Value()
				if m.query == "" {
					m.err = fmt.Errorf("empty query")
					return m, nil
				}
				m.state = stateResults
				m.paginator.Page = 0 // Reset to page 1
				m.page = 1
				return m, m.fetchSearchResults()
			case "down":
				if len(m.autoList.Items()) > 0 {
					m.autoList, cmd = m.autoList.Update(msg)
					return m, cmd
				}
			case "up":
				if len(m.autoList.Items()) > 0 {
					m.autoList, cmd = m.autoList.Update(msg)
					return m, cmd
				}
			default:
				m.input, cmd = m.input.Update(msg)
				query := m.input.Value()
				if utf8.RuneCountInString(query) >= 3 { // Debounce/threshold for autocomplete
					return m, tea.Batch(cmd, m.fetchAutocomplete(query))
				}
				return m, cmd
			}
		case stateResults:
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "esc":
				m.state = stateInput
				m.input.Focus()
				m.problem = ""
				m.suggestions = nil
				m.err = nil
				return m, nil
			case "n", "right":
				if m.paginator.Page < m.paginator.TotalPages-1 {
					m.paginator.Page++
					m.page = m.paginator.Page + 1
					return m, m.fetchSearchResults()
				}
			case "p", "left":
				if m.paginator.Page > 0 {
					m.paginator.Page--
					m.page = m.paginator.Page + 1
					return m, m.fetchSearchResults()
				}
			case "enter":
				if len(m.suggestions) > 0 {
					if item := m.resultsList.SelectedItem(); item != nil {
						selected := item.(resultItem).logicalTitle
						if selected == "Ignore suggestions and show original results" {
							m.suggestions = nil
							m.problem = ""
							m.resultsList.SetItems(m.originalItems)
							return m, nil
						} else {
							m.query = selected
							m.problem = ""
							m.suggestions = nil
							m.page = 1
							return m, m.fetchSearchResults()
						}
					}
				}
			default:
				m.resultsList, cmd = m.resultsList.Update(msg)
				return m, cmd
			}
		}
	case searchResultsMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.totalHits = msg.totalHits
		m.totalPages = (msg.totalHits + pageSize - 1) / pageSize
		m.paginator.TotalPages = m.totalPages
		m.problem = msg.problem
		m.suggestions = msg.suggestions
		m.originalItems = []list.Item{}
		for _, res := range msg.results {
			m.originalItems = append(m.originalItems, resultItem{logicalTitle: res.Title, logicalDesc: res.URL})
		}
		items := m.originalItems
		if len(m.suggestions) > 0 {
			items = []list.Item{}
			for _, sugg := range m.suggestions {
				items = append(items, resultItem{logicalTitle: sugg, logicalDesc: "Suggested correction - press Enter to search"})
			}
			items = append(items, resultItem{logicalTitle: "Ignore suggestions and show original results", logicalDesc: "Press Enter to view"})
		}
		m.resultsList.SetItems(items)
		if len(items) == 0 {
			m.err = fmt.Errorf(noResultsMsg)
		}
		return m, nil
	case autoCompleteMsg:
		if msg.err != nil {
			return m, nil // Silent fail
		}
		items := []list.Item{}
		for _, res := range msg.results {
			suffix := res.Suffix
			if suffix != "" {
				items = append(items, resultItem{logicalTitle: m.input.Value() + suffix, logicalDesc: res.Title})
			}
		}
		m.autoList.SetItems(items)
		return m, nil
	}
	switch m.state {
	case stateInput:
		m.input, cmd = m.input.Update(msg)
	case stateResults:
		m.resultsList, cmd = m.resultsList.Update(msg)
		m.paginator, _ = m.paginator.Update(msg) // No cmd from paginator
	}
	return m, cmd
}

func (m model) View() string {
	switch m.state {
	case stateInput:
		view := inputStyle.Render(m.input.View())
		if len(m.autoList.Items()) > 0 {
			view += "\n" + listStyle.Render(m.autoList.View())
		}
		if m.err != nil {
			view += "\n" + errorStyle.Render(m.err.Error())
		}
		return view + "\nEnter to search, q to quit"
	case stateResults:
		view := fmt.Sprintf("Results for '%s' (%d hits)\n", bidiReorder(m.query), m.totalHits)
		if m.problem != "" {
			view += suggestStyle.Render(bidiReorder(m.problem) + "\n" + bidiReorder("Select a suggestion below:\n"))
		}
		view += listStyle.Render(m.resultsList.View())
		view += "\n" + m.paginator.View()
		if m.err != nil {
			view += "\n" + errorStyle.Render(m.err.Error())
		}
		return view + "\nEsc to back, q to quit, n/p for pages"
	}
	return ""
}

// Cmds and Msgs
type searchResultsMsg struct {
	results     []searchResult
	totalHits   int
	problem     string
	suggestions []string
	err         error
}

type autoCompleteMsg struct {
	results []searchResult
	err     error
}

func (m model) fetchAutocomplete(query string) tea.Cmd {
	return func() tea.Msg {
		resp, err := http.Get(apiBase + autoComplete + query)
		if err != nil {
			return autoCompleteMsg{err: err}
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var apiRes apiResponse
		json.Unmarshal(body, &apiRes)
		return autoCompleteMsg{results: apiRes.Results}
	}
}

func (m model) fetchSearchResults() tea.Cmd {
	return func() tea.Msg {
		q := strings.ReplaceAll(m.query, " ", "+") // Parse/correct: spaces to +
		url := fmt.Sprintf("%s%s%s&page=%d", apiBase, search, q, m.page)
		resp, err := http.Get(url)
		if err != nil {
			return searchResultsMsg{err: err}
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var apiRes apiResponse
		json.Unmarshal(body, &apiRes)
		var suggs []string
		for _, s := range apiRes.Sugg {
			if cq, ok := s["corrected_query"].(string); ok {
				suggs = append(suggs, cq)
			}
		}
		return searchResultsMsg{
			results:     apiRes.Results,
			totalHits:   apiRes.TotalHits,
			problem:     apiRes.Problem,
			suggestions: suggs,
		}
	}
}

// List Item
type resultItem struct {
	logicalTitle, logicalDesc string
}

func (i resultItem) Title() string       { return bidiReorder(i.logicalTitle) }
func (i resultItem) Description() string { return bidiReorder(i.logicalDesc) }
func (i resultItem) FilterValue() string { return i.logicalTitle }

func bidiReorder(s string) string {
	p := bidi.Paragraph{}
	_, err := p.SetString(s, bidi.DefaultDirection(bidi.RightToLeft))
	if err != nil {
		return s
	}
	o, err := p.Order()
	if err != nil {
		return s
	}
	var visual []rune
	for i := 0; i < o.NumRuns(); i++ {
		run := o.Run(i)
		rs := []rune(run.String())
		if run.Direction() == bidi.RightToLeft {
			for j, k := 0, len(rs)-1; j < k; j, k = j+1, k-1 {
				rs[j], rs[k] = rs[k], rs[j]
			}
		}
		visual = append(visual, rs...)
	}
	return string(visual)
}

func StartTUI() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("Error running program:", err)
	}
}
