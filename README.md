# LLM Search Engine

Imagine if you had no internet access but wanted to search the web... I mean yeah you could use an offline llm but that just doesnt feel as good as a website now does it? Fear no more the product no one was asking for is here! 

This is a pseudo search engine that promps an LLM to answer your question/prompt by generating an entire offline website.

The application supports dark mode, dynamic model selection, and a special "thinking mode" where additional reasoning is displayed in a dedicated popup.

Bugs? has plenty!

## Features

- **LLM Integration:**  
  Uses a streaming LLM (e.g., via an Ollama API) to generate fully formed HTML pages based on search queries.

- **Dynamic Model Selection:**  
  Lets users choose from a list of models provided by the Ollama instance, dynamically fetching the available models to ensure up-to-date options.

- **Dark Mode Support:**  
  The modern interface supports light and dark themes. When in dark mode, the application notifies the backend so the generated page matches the dark theme.

- **Thinking Mode:**  
  Optionally, if the selected model produces additional reasoning enclosed within `<think>...</think>` tags, this content is extracted and shown in a pop-up accessible via a star icon.

- **Modern UI:**  
  The front end features a clean, responsive design with a settings panel that allows users to configure connection settings, model selection, theme toggling, and enable "thinking" mode.

- **Single Binary Deployment:**  
  Uses Go’s `embed` package to include HTML templates within the binary, simplifying deployment.

This was mainly created for me to benchmark LLM's all out ability ie coding svg generation knowledge etc. but its also fun.

Can ya tell an LLM Wrote this Readme? well I didnt want to...
Its probably fine...



## Installation

### Prerequisites

- [Go 1.16+](https://golang.org/dl/) must be installed on your system.
- An instance of [Ollama](https://ollama.ai/) or any compatible LLM API endpoint should be running.

### Steps

1. **Clone the Repository:**

    ```sh
    git clone https://github.com/mrdavidaylward/LLMSearchEngine.git
    cd LLMSearchEngine
    ```

2. **Initialize Go Modules:**

    ```sh
    go mod tidy
    ```

## Usage

Start the server by running:

```sh
go run main.go
```

By default, the server listens on http://localhost:8080. Open this URL in your browser to interact with the search engine.

Configuration
Ollama URL:
The app defaults to http://localhost:11434 for the Ollama API endpoint. You can change the URL via the settings panel.

Model Selection:
The default model is set to llama3:8b. Select a different model from the dynamically populated model list provided by your Ollama instance.

Thinking Mode:
Enable or disable "Thinking model" mode via a checkbox in the settings panel. When enabled, <think>...</think> blocks in the generated output are extracted and displayed in a popup.

Dark Mode:
Toggle dark mode from the settings panel. The current theme is sent as a hidden parameter to the backend so that the LLM generates a matching dark mode page.

Contributing
Contributions are welcome! To contribute:

Fork the repository.

Create a new branch for your feature or bug fix.

Make your changes and commit them with clear messages.

Submit a pull request for review.
