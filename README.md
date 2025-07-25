# Dokeep: The Intelligent Document Library
[![Docker Image](https://github.com/nushankodikara/dokeep/actions/workflows/docker-publish.yaml/badge.svg)](https://github.com/nushankodikara/dokeep/actions/workflows/docker-publish.yaml)

Dokeep is a self-hosted document management system built with Go and Python. It allows you to upload, analyze, and search your documents with ease. The system uses a chain of AI services to perform OCR, extract key metadata, and intelligently tag and summarize your content.

## Features

-   **Multi-Format Upload:** Supports PDF, JPG, and PNG documents with a drag-and-drop interface.
-   **Duplicate File Detection:** Prevents duplicate documents by checking the hash of file contents.
-   **Automatic OCR:** All uploaded documents are automatically scanned to extract their text content.
-   **AI-Powered Analysis (via Ollama):**
    -   **Intelligent Date Extraction:** Automatically finds and sets the document's creation date from its content, understanding formats like "January 1st, 2023".
    -   **Automatic Tagging:** A two-stage process first uses a classic ML model for initial tags, which are then refined by an LLM for higher accuracy.
    -   **Automatic Summarization:** If you don't provide a summary, the LLM will generate a concise one for you.
-   **Powerful Search:** Full-text search across titles, summaries, extracted content, and tags.
-   **Secure Authentication:** User accounts with Two-Factor Authentication (TOTP) for enhanced security.
-   **Dockerized Environment:** Comes with a full Docker and Docker Compose setup for easy deployment.
-   **CI/CD Ready:** Includes a GitHub Actions workflow to automatically build and publish Docker images for all services.

## Tech Stack

-   **Backend:** Go
-   **Frontend:** Templ (Go-based HTML templating), TailwindCSS, Alpine.js
-   **OCR & ML Service:** Python (FastAPI) for OCR, initial date extraction, and classic ML-based tagging.
-   **LLM Service:** Python (FastAPI) for advanced analysis, using **Ollama** to run the `qwen2:0.5b` model.
-   **Database:** PostgreSQL
-   **Containerization:** Docker & Docker Compose

## Getting Started
The only supported way to run Dokeep is by using Docker.

### Prerequisites

-   Docker & Docker Compose
-   **Ollama:** The LLM service requires the [Ollama desktop application](https://ollama.com/) to be running on the host machine.

### Configuring the Ollama Host

The LLM service connects to the Ollama instance running on your host machine. The default URL is set to `http://host.docker.internal:11434`. If your Ollama instance is running on a different address, you can change the `OLLAMA_HOST` environment variable in the `docker-compose.yml` and `docker-compose.local.yaml` files.

### Running for Local Development

This method builds the Docker images from your local source code, which is ideal when you are actively developing and making changes.

1.  **Build and start the containers:**
    From the root of the project, run the following command:
    ```bash
    docker-compose -f docker-compose.yml -f docker-compose.local.yaml up --build
    ```
    -   `-f docker-compose.yml -f docker-compose.local.yaml`: Merges the production and local configurations, telling Docker Compose to build the images from source.
    -   `--build`: Forces a rebuild of the images to include any recent code changes.

2.  **Access the application:**
    Once the build is complete and the containers are running, the application will be available at `http://localhost:8081`.

3.  **Stopping the application:**
    Press `Ctrl+C` in the terminal where the containers are running, or run `docker-compose down` to stop them if they are in detached mode.

### Running in Production

This method pulls the pre-built, stable images from a container registry (like Docker Hub). It's faster and is the standard way to deploy the application to a server.

1.  **Pull and start the containers:**
    ```bash
    docker-compose up -d
    ```
    -   `-d`: Runs the containers in detached mode (in the background).

2.  **Stopping the application:**
    ```bash
    docker-compose down
    ```

## CI/CD

The project includes a GitHub Actions workflow (`.github/workflows/docker-publish.yaml`) that automatically builds and pushes the Docker images for both the Go application and the Python service to Docker Hub whenever changes are pushed to the `main` branch.

## LLM Service Configuration (Ollama vs. OpenAI)

The LLM service is designed to be flexible. By default, it uses a local Ollama instance to run the `qwen2:0.5b` model. However, you can easily switch to using OpenAI's more powerful `gpt-4o` model by providing an API key.

### To Use OpenAI:

1.  **Create a `.env` file:** In the root of the project, create a file named `.env`.
2.  **Add your configuration:** Add your OpenAI API key and, optionally, the model you wish to use.
    ```
    OPENAI_API_KEY=sk-your-api-key-here
    OPENAI_MODEL=gpt-4-turbo
    ```
3.  **Restart the services:** If the application is running, restart it with `docker-compose up -d --build`.

### To Use a Different Ollama Model:

To use a different model from Ollama (e.g., `llama3`), you can set the `OLLAMA_MODEL` variable in your `.env` file:
```
OLLAMA_MODEL=llama3
```
Then, restart the services. The `llm-service` will pull and use the specified model.

If no model is specified, the service defaults to `gpt-4o` for OpenAI and `qwen2:0.5b` for Ollama.

### Configuring the Ollama Host

When using Ollama, the `llm-service` needs to know how to connect to the Ollama instance running on your host machine. This is configured via the `OLLAMA_HOST` environment variable.

-   **For Mac and Windows:** Docker's `host.docker.internal` DNS name is typically used. The default value is already set for this.
-   **For Linux:** You may need to find your host's IP address on the Docker bridge network. You can often find this by running `ip addr show docker0` and looking for the IP address.

To override the default, add the `OLLAMA_HOST` variable to your `.env` file:
```
OLLAMA_HOST=http://172.17.0.1:11434
```

### Disabling AI Features

You can disable all AI-powered features (such as content analysis and tagging) by setting the `DISABLE_AI` environment variable to `1` in your `.env` file:

```
DISABLE_AI=1
```
By default, AI features are enabled (`DISABLE_AI=0`).

## Project Structure

```
.
├── cmd/dokeep/          # Main Go application entrypoint
├── internal/              # Go application's core logic
├── llm-service/           # Python service for LLM analysis via Ollama
├── py-service/            # Python microservice for OCR and classic ML
├── uploads/               # Storage for uploaded files and thumbnails (managed by a Docker volume)
├── web/                   # Frontend templates and components
├── .github/workflows/     # CI/CD workflows
├── Dockerfile             # Dockerfile for the Go application
├── docker-compose.yml     # Production Docker Compose file
└── README.md
``` 