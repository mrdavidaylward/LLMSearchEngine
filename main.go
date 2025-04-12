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
	// "time" // Removed unused import

	_ "embed" // Package for embedding files into the binary
)

//go:embed templates/search.html
var searchTemplateRaw string // Embed the search.html template content

// OllamaRequest represents the request body for Ollama API /api/generate
type OllamaRequest struct {
	Model  string `json:"model"`  // The model to use (e.g., "llama3:8b")
	Prompt string `json:"prompt"` // The input prompt for the model
	Stream bool   `json:"stream"` // Whether to stream the response
}

// OllamaResponse represents a chunk of the streamed response from Ollama API
// Updated to include token count fields potentially present in the *final* chunk.
type OllamaResponse struct {
	Model           string `json:"model"`            // Model that generated the response
	CreatedAt       string `json:"created_at"`       // Timestamp of creation
	Response        string `json:"response"`         // The actual content chunk
	Done            bool   `json:"done"`             // True if this is the final chunk
	PromptEvalCount int    `json:"prompt_eval_count"`// Tokens in the prompt
	EvalCount       int    `json:"eval_count"`       // Tokens in the response
	// Add other fields from the final summary if needed, e.g., total_duration
}

// PageData holds data passed to the HTML template for rendering
// Updated to include token counts.
type PageData struct {
	Query          string        // The user's search query
	Content        template.HTML // The generated HTML content from Ollama
	IsSearch       bool          // Flag indicating if this is a search results page or the initial page
	Debug          string        // Debugging information from the generation process
	OllamaURL      string        // URL of the Ollama instance being used
	ModelList      []string      // List of available models for the dropdown
	CurrentModel   string        // The model selected for the current request
	ThinkingFlag   bool          // Flag indicating if "thinking" content should be extracted
	SvgFlag        bool          // Flag indicating if SVG generation was requested
	ThinkContent   template.HTML // Extracted content from <think> tags
	Theme          string        // Current theme ("light" or "dark")
	PromptTokens   int           // Number of tokens in the prompt (from Ollama)
	ResponseTokens int           // Number of tokens in the response (from Ollama)
}

// Constants for default values
const (
	defaultOllamaURL = "http://localhost:11434" // Default URL for the Ollama service
	defaultModel     = "llama3:8b"             // Default model to use
)

// defaultModels provides a static list of models for the UI dropdown
var defaultModels = []string{
	"llama3:8b",
	"llama3:70b",
	"mistral:7b",
	"gemma:7b",
}

// stripFences removes leading/trailing Markdown code fences (``` or ```html) from the content.
// This is useful if the LLM wraps its HTML output in fences.
func stripFences(content string) string {
	// Regex to match lines that are exactly ``` or ```html, potentially with surrounding whitespace
	fenceLine := regexp.MustCompile("^\\s*```(?:html)?\\s*$")
	lines := strings.Split(content, "\n")
	start, end := 0, len(lines)

	// Remove leading fence line if present
	if start < end && fenceLine.MatchString(lines[start]) {
		start++
	}
	// Remove trailing fence line if present
	if start < end && fenceLine.MatchString(lines[end-1]) {
		end--
	}
	// Join the remaining lines back together
	return strings.Join(lines[start:end], "\n")
}

// isThinkingModel checks if the model name suggests it might include <think> tags.
func isThinkingModel(model string) bool {
	// Simple check if the model name contains "think" (case-insensitive)
	return strings.Contains(strings.ToLower(model), "think")
}

// extractThinkContent finds all <think>...</think> blocks in the HTML,
// removes them from the main content, and concatenates their inner text.
func extractThinkContent(html string) (cleaned string, thinkText string) {
	// Regex to find <think> tags and capture their content (case-insensitive, dot matches newline)
	re := regexp.MustCompile(`(?is)<think>(.*?)</think>`)
	var thinks []string // Slice to store the extracted think texts

	// Replace all occurrences of <think> blocks
	cleaned = re.ReplaceAllStringFunc(html, func(m string) string {
		// Extract the content inside the tags
		sub := re.FindStringSubmatch(m)
		if len(sub) > 1 {
			think := strings.TrimSpace(sub[1]) // Trim whitespace
			if think != "" {
				thinks = append(thinks, think) // Add non-empty think text to the list
			}
		}
		return "" // Replace the <think> block with an empty string
	})
	// Join all extracted think texts with double newlines
	thinkText = strings.Join(thinks, "\n\n")
	return
}

// processRelatedTopics modifies links within the "Related Topics" section.
// It changes the href to "#" and adds a data-topic attribute containing the link text.
// This allows frontend JavaScript to handle clicks on these links (e.g., to trigger a new search).
func processRelatedTopics(content string) string {
	// Regex to find <a> tags with an href attribute (case-insensitive)
	re := regexp.MustCompile(`(?i)<a\s+href=["']?([^"'>\s]+)["']?>([^<]+)</a>`)

	// Replace matching links
	return re.ReplaceAllStringFunc(content, func(m string) string {
		// Extract the link text (the part between > and <)
		if sm := regexp.MustCompile(`>([^<]+)<`).FindStringSubmatch(m); len(sm) > 1 {
			topic := sm[1] // The extracted topic text
			// Return a new link with href="#" and data-topic attribute
			return fmt.Sprintf(`<a href="#" data-topic="%s">%s</a>`, topic, topic)
		}
		// If extraction fails, return the original match
		return m
	})
}

// generateContent interacts with the Ollama API to generate HTML content.
// Updated to return prompt and response token counts along with HTML and debug info.
func generateContent(ctx context.Context, query, ollamaURL, model, theme string, svg bool) (htmlContent string, promptTokens int, responseTokens int, debugInfo string) {
	var debug strings.Builder // Use strings.Builder for efficient string concatenation
	fmt.Fprintf(&debug, "Query: %s\nModel: %s\nURL: %s\nTheme: %s\nSVG: %t\n\n", query, model, ollamaURL, theme, svg)

	// Base prompt instructing the LLM on the desired output format and constraints
	basePrompt := fmt.Sprintf(`You are a helpful AI assistant that generates visually apealing entire web pages in response to search queries.
Generate a complete webpage (using HTML) that thoroughly answers the query: "%s"
DO NOT!:
1.Include any explanations or comments in the code.
2.Include external links.

Your response should:
1. Have a professional design with CSS included in a <style> tag.
2. Include a proper heading structure (h1, h2, etc.).
3. Provide comprehensive information on the topic.
4. Include visual structure with sections, lists, or tables as appropriate.
5. Be factually accurate and educational.
6. Format code examples properly if relevant.
7. Include a "Related Topics" section at the bottom with at least 3-5 related topic links.
8. any charts should be self contained chart.js graphs.
`, query)

	// Append theme-specific instructions
	if theme == "dark" {
		basePrompt += "\nNote: The webpage must be designed for dark mode. Use a dark color palette and design elements that align with a dark interface."
	}

	// Append SVG-specific instruction if enabled
	if svg {
		basePrompt += "\nAdditionally, Generate any images carefully as self-contained detailed SVG images that do not depend on external resources."
	}

	// Prepare the request body for the Ollama API
	reqBody := OllamaRequest{Model: model, Prompt: basePrompt, Stream: true} // Enable streaming
	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		debug.WriteString("Error marshaling request: " + err.Error())
		return "<p>Error generating content (request marshal).</p>", 0, 0, debug.String()
	}

	// Create the HTTP request to the Ollama API
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ollamaURL+"/api/generate", bytes.NewBuffer(reqJSON))
	if err != nil {
		debug.WriteString("Error creating request: " + err.Error())
		return "<p>Error generating content (request creation).</p>", 0, 0, debug.String()
	}
	req.Header.Set("Content-Type", "application/json")

	// Send the request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		debug.WriteString("Ollama request error: " + err.Error())
		// Provide a more user-friendly error message
		htmlContent = fmt.Sprintf("<p>Error connecting to LLM service at %s. Is it running?</p>", ollamaURL)
		return htmlContent, 0, 0, debug.String()
	}
	defer resp.Body.Close() // Ensure the response body is closed

	// Check for non-OK status codes
	if resp.StatusCode != http.StatusOK {
		debug.WriteString(fmt.Sprintf("Non-200 status: %d\n", resp.StatusCode))
		// Attempt to read the response body for more details
		bodyBytes, _ := bufio.NewReader(resp.Body).ReadBytes('\n') // Read first line or up to buffer size
		debug.WriteString(fmt.Sprintf("Response body hint: %s\n", string(bodyBytes)))
		htmlContent = fmt.Sprintf("<p>LLM service returned error code: %d.</p>", resp.StatusCode)
		return htmlContent, 0, 0, debug.String()
	}

	// Process the streamed response
	var fullResponseContent strings.Builder
	var lastChunkBytes []byte // Store the raw bytes of the last received chunk
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		chunkBytes := scanner.Bytes()
		// Make a copy because scanner.Bytes() buffer can be overwritten
		currentChunkBytes := make([]byte, len(chunkBytes))
		copy(currentChunkBytes, chunkBytes)
		lastChunkBytes = currentChunkBytes // Keep track of the latest chunk bytes

		var chunk OllamaResponse
		// Attempt to unmarshal each line (chunk) of the response
		if err := json.Unmarshal(chunkBytes, &chunk); err == nil {
			fullResponseContent.WriteString(chunk.Response) // Append the content part of the chunk
			if chunk.Done {
				// We could potentially break here if we only needed the final chunk stats,
				// but we still need to ensure the fullResponseContent is complete.
				// The loop naturally ends anyway.
			}
		} else {
			// Log unmarshal errors but continue processing if possible
			debug.WriteString(fmt.Sprintf("Warn: Error unmarshaling chunk: %v\nChunk: %s\n", err, string(chunkBytes)))
		}
		// Check context cancellation (e.g., client closed connection)
		if ctx.Err() != nil {
			debug.WriteString("Context cancelled during stream processing.\n")
			return "<p>Request cancelled.</p>", 0, 0, debug.String() // Stop processing if context is done
		}
	}
	// Check for errors during scanning (e.g., network issues)
	if err := scanner.Err(); err != nil {
		debug.WriteString("Error reading response stream: " + err.Error())
		// Don't necessarily discard already received content, but log the error
	}

	// Process the final chunk to extract token counts
	var finalStats OllamaResponse
	if len(lastChunkBytes) > 0 {
		if err := json.Unmarshal(lastChunkBytes, &finalStats); err == nil {
			if finalStats.Done {
				promptTokens = finalStats.PromptEvalCount
				responseTokens = finalStats.EvalCount
				debug.WriteString(fmt.Sprintf("\nFinal chunk stats: Done=%t, PromptTokens=%d, ResponseTokens=%d\n",
					finalStats.Done, promptTokens, responseTokens))
			} else {
				debug.WriteString("\nWarn: Last received chunk was not marked as done.\n")
			}
		} else {
			debug.WriteString(fmt.Sprintf("\nWarn: Error unmarshaling final chunk: %v\nFinal Chunk: %s\n", err, string(lastChunkBytes)))
		}
	} else {
		debug.WriteString("\nWarn: No response chunks received.\n")
	}

	htmlContent = fullResponseContent.String()
	htmlContent = stripFences(htmlContent) // Clean up potential code fences
	debugInfo = debug.String()
	return // Return named return values
}

func main() {
	// Parse the embedded HTML template
	// Using Must ensures that if the template is invalid, the program panics at startup
	tmpl := template.Must(template.New("search.html").Parse(searchTemplateRaw))

	// Handler for the root path ("/") - displays the initial search form
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Basic data for the initial page
		data := PageData{
			IsSearch:     false,
			OllamaURL:    defaultOllamaURL,
			ModelList:    defaultModels, // Use the hardcoded list
			CurrentModel: defaultModel,
			ThinkingFlag: false,
			SvgFlag:      false,
			Theme:        "light", // Default theme
			// Token counts are not applicable/available on the initial page
			PromptTokens:   -1, // Use -1 or similar to indicate not available
			ResponseTokens: -1,
		}
		// Execute the template with the data
		err := tmpl.Execute(w, data)
		if err != nil {
			log.Printf("Template execution error for /: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	// Handler for the "/search" path - performs the search and displays results
	http.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		// Extract query parameters
		q := r.URL.Query().Get("q")
		if q == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther) // Redirect to home if query is empty
			return
		}
		ollamaURL := r.URL.Query().Get("ollama_url")
		if ollamaURL == "" {
			ollamaURL = defaultOllamaURL // Use default if not provided
		}
		model := r.URL.Query().Get("model")
		if model == "" {
			model = defaultModel // Use default if not provided
		}
		thinkingParam := r.URL.Query().Get("thinking_model")
		thinkingFlag := thinkingParam == "true"

		svgParam := r.URL.Query().Get("svg")
		svgFlag := svgParam == "true"

		theme := r.URL.Query().Get("theme")
		if theme != "dark" { // Default to light unless explicitly "dark"
			theme = "light"
		}

		// Generate the content using the Ollama API
		// Capture returned token counts
		rawHTML, promptTokens, responseTokens, debug := generateContent(r.Context(), q, ollamaURL, model, theme, svgFlag)

		// Post-process the generated HTML
		rawHTML = processRelatedTopics(rawHTML) // Make related topics clickable

		var thinkHTML string
		// Check if thinking content needs extraction (either by flag or model name)
		if thinkingFlag || isThinkingModel(model) {
			cleaned, think := extractThinkContent(rawHTML)
			rawHTML = cleaned // Use the HTML with <think> tags removed
			thinkHTML = think // Store the extracted think content
		}

		// Prepare data for the results page template
		data := PageData{
			Query:          q,
			Content:        template.HTML(rawHTML), // Mark the generated HTML as safe for templating
			IsSearch:       true,
			Debug:          debug, // Pass debug information to the template
			OllamaURL:      ollamaURL,
			ModelList:      defaultModels, // Provide model list again for the form
			CurrentModel:   model,
			ThinkingFlag:   thinkingFlag,
			SvgFlag:        svgFlag,
			ThinkContent:   template.HTML(thinkHTML), // Pass extracted think content
			Theme:          theme,
			PromptTokens:   promptTokens,   // Pass prompt token count
			ResponseTokens: responseTokens, // Pass response token count
		}

		// Execute the template with the search results data
		err := tmpl.Execute(w, data)
		if err != nil {
			// Log the error that occurred during template execution
			log.Printf("Template execution error for /search: %v", err)
			// Send a generic error response. http.Error checks if headers are written.
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			// Removed the incorrect Hijacker check here.
		}
	})

	port := ":8080"
	fmt.Printf("Server starting on port %s...\n", port)
	// Start the HTTP server
	log.Fatal(http.ListenAndServe(port, nil)) // log.Fatal logs and exits if ListenAndServe fails
}
