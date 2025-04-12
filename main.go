package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	_ "embed"
)

//go:embed templates/search.html
var searchTemplateRaw string

// OllamaRequest represents the request body for Ollama API
type OllamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// OllamaResponse represents a chunk of the response from Ollama API
type OllamaResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Response  string `json:"response"`
	Done      bool   `json:"done"`
}

// PageData holds data passed to the template
type PageData struct {
	Query         string
	Content       template.HTML
	IsSearch      bool
	Debug         string
	OllamaURL     string
	ModelList     []string
	CurrentModel  string
	ThinkingFlag  bool
	ThinkContent  template.HTML
	Theme         string // "light" or "dark"
}

const (
	defaultOllamaURL = "http://localhost:11434"
	defaultModel     = "llama3:8b"
)

var defaultModels = []string{
	"llama3:8b",
	"llama3:70b",
	"mistral:7b",
	"gemma:7b",
}

// stripFences removes any leading/trailing lines that are exactly ``` or ```html
func stripFences(content string) string {
	fenceLine := regexp.MustCompile("^\\s*```(?:html)?\\s*$")
	lines := strings.Split(content, "\n")
	start, end := 0, len(lines)
	if start < end && fenceLine.MatchString(lines[start]) {
		start++
	}
	if start < end && fenceLine.MatchString(lines[end-1]) {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}

// isThinkingModel returns true if the model name indicates it's a “thinking” model
func isThinkingModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "think")
}

// extractThinkContent pulls out all <think>…</think> blocks,
// returns the cleaned HTML (with them removed) and the concatenated think‑texts.
func extractThinkContent(html string) (cleaned string, thinkText string) {
	re := regexp.MustCompile(`(?is)<think>(.*?)</think>`)
	var thinks []string
	cleaned = re.ReplaceAllStringFunc(html, func(m string) string {
		sub := re.FindStringSubmatch(m)
		if len(sub) > 1 {
			think := strings.TrimSpace(sub[1])
			if think != "" {
				thinks = append(thinks, think)
			}
		}
		return ""
	})
	thinkText = strings.Join(thinks, "\n\n")
	return
}

// processRelatedTopics makes links in the "Related Topics" section clickable via data-topic
func processRelatedTopics(content string) string {
	re := regexp.MustCompile(`(?i)<a\s+href=["']?([^"'>\s]+)["']?>([^<]+)</a>`)
	return re.ReplaceAllStringFunc(content, func(m string) string {
		if sm := regexp.MustCompile(`>([^<]+)<`).FindStringSubmatch(m); len(sm) > 1 {
			topic := sm[1]
			return fmt.Sprintf(`<a href="#" data-topic="%s">%s</a>`, topic, topic)
		}
		return m
	})
}

// generateContent calls Ollama to produce the page HTML.
// It takes the theme parameter so that, if dark mode is active,
// the prompt instructs the model to generate a dark mode–friendly page.
func generateContent(ctx context.Context, query, ollamaURL, model, theme string) (string, string) {
	var debug strings.Builder
	fmt.Fprintf(&debug, "Query: %s\nModel: %s\nURL: %s\nTheme: %s\n\n", query, model, ollamaURL, theme)

	basePrompt := fmt.Sprintf(`You are a helpful AI assistant that generates entire web pages in response to search queries.
Generate a complete webpage (using HTML) that thoroughly answers the query: "%s"

Your response should:
1. Have a professional design with CSS included in a <style> tag.
2. Include a proper heading structure (h1, h2, etc.).
3. Provide comprehensive information on the topic.
4. Include visual structure with sections, lists, or tables as appropriate.
5. Be factually accurate and educational.
6. Format code examples properly if relevant.
7. Include a "Related Topics" section at the bottom with at least 3-5 related topic links.
`, query)

	// Append a note for dark mode
	if theme == "dark" {
		basePrompt += "\nNote: The webpage must be designed for dark mode. Use a dark color palette and design elements that align with a dark interface."
	}

	reqBody := OllamaRequest{Model: model, Prompt: basePrompt, Stream: true}
	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		debug.WriteString("Error marshaling request: " + err.Error())
		return "<p>Error generating content.</p>", debug.String()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ollamaURL+"/api/generate", bytes.NewBuffer(reqJSON))
	if err != nil {
		debug.WriteString("Error creating request: " + err.Error())
		return "<p>Error generating content.</p>", debug.String()
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		debug.WriteString("Ollama request error: " + err.Error())
		return "<p>Error connecting to LLM service.</p>", debug.String()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		debug.WriteString(fmt.Sprintf("Non-200 status: %d\n", resp.StatusCode))
		return "<p>LLM returned error code.</p>", debug.String()
	}

	var full strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var chunk OllamaResponse
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err == nil {
			full.WriteString(chunk.Response)
		}
	}
	if err := scanner.Err(); err != nil {
		debug.WriteString("Error reading response stream: " + err.Error())
	}
	content := full.String()
	content = stripFences(content)
	return content, debug.String()
}

func main() {
	tmpl, err := template.New("search.html").Parse(searchTemplateRaw)
	if err != nil {
		log.Fatalf("template parse error: %v", err)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data := PageData{
			IsSearch:     false,
			OllamaURL:    defaultOllamaURL,
			ModelList:    defaultModels,
			CurrentModel: defaultModel,
			ThinkingFlag: false,
			Theme:        "light",
		}
		tmpl.Execute(w, data)
	})

	http.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		ollamaURL := r.URL.Query().Get("ollama_url")
		if ollamaURL == "" {
			ollamaURL = defaultOllamaURL
		}
		model := r.URL.Query().Get("model")
		if model == "" {
			model = defaultModel
		}
		thinkingParam := r.URL.Query().Get("thinking_model")
		thinkingFlag := thinkingParam == "true"

		theme := r.URL.Query().Get("theme")
		if theme == "" {
			theme = "light"
		}

		// Artificial delay for spinner
		time.Sleep(1 * time.Second)

		rawHTML, debug := generateContent(r.Context(), q, ollamaURL, model, theme)
		rawHTML = processRelatedTopics(rawHTML)

		var thinkHTML string
		if thinkingFlag || isThinkingModel(model) {
			cleaned, think := extractThinkContent(rawHTML)
			rawHTML = cleaned
			thinkHTML = think
		}

		data := PageData{
			Query:        q,
			Content:      template.HTML(rawHTML),
			IsSearch:     true,
			Debug:        debug,
			OllamaURL:    ollamaURL,
			ModelList:    defaultModels,
			CurrentModel: model,
			ThinkingFlag: thinkingFlag,
			ThinkContent: template.HTML(thinkHTML),
			Theme:        theme,
		}
		tmpl.Execute(w, data)
	})

	fmt.Println("Server starting on :8080…")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
