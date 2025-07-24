# Dokeep: The Intelligent Document Library
[![Docker Image](https://github.com/nushankodikara/dokeep/actions/workflows/docker-publish.yaml/badge.svg)](https://github.com/nushankodikara/dokeep/actions/workflows/docker-publish.yaml)

Dokeep is a self-hosted document management system built with Go and Python. It allows you to upload, analyze, and search your documents with ease. The system automatically performs OCR on your files, extracts dates, and allows for powerful searching across all your content.

## Features

-   **Multi-Format Upload:** Supports PDF, JPG, and PNG documents.
-   **Automatic OCR:** All uploaded documents are automatically scanned to extract their text content.
-   **Intelligent Date Extraction:** Automatically finds and sets the document's creation date from its content.
-   **Powerful Search:** Full-text search across titles, summaries, extracted content, and tags.
-   **Tag Management:** Organize your documents with custom tags.
-   **Secure Authentication:** User accounts with Two-Factor Authentication (TOTP) for enhanced security.
-   **Dockerized Environment:** Comes with a full Docker and Docker Compose setup for easy deployment.
-   **CI/CD Ready:** Includes a GitHub Actions workflow to automatically build and publish Docker images.

## Tech Stack

-   **Backend:** Go
-   **Frontend:** Templ (Go-based HTML templating), TailwindCSS, Alpine.js
-   **Microservice:** Python (FastAPI) for OCR, date extraction, and machine learning tasks.
-   **Database:** SQLite
-   **Containerization:** Docker & Docker Compose

## Getting Started (Local Development)

### Prerequisites

-   Go (1.24+)
-   Python (3.9+)
-   Tesseract OCR Engine
-   `poppler-utils` (for PDF processing)
-   Docker & Docker Compose (for the containerized setup)

### Installation & Running

1.  **Clone the repository:**
    ```bash
    git clone <your-repo-url>
    cd dokeep
    ```

2.  **Run the Python Microservice:**
    ```bash
    cd py-service
    pip install -r requirements.txt
    uvicorn main:app --host 0.0.0.0 --port 8000
    ```

3.  **Run the Go Application (in a separate terminal):**
    ```bash
    go run ./cmd/dokeep
    ```

The application will be available at `http://localhost:8081`.

## Docker Deployment

This is the recommended way to run Dokeep, as it encapsulates all services and dependencies into managed containers.

### Prerequisites

-   Docker
-   Docker Compose

Make sure both Docker and Docker Compose are installed on your system before proceeding.

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
    Press `Ctrl+C` in the terminal where the containers are running.

### Running in Production

This method pulls the pre-built, stable images from Docker Hub. It's faster and is the standard way to deploy the application to a server.

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

## Project Structure

```
.
├── cmd/dokeep/          # Main Go application entrypoint
├── data/                  # SQLite database storage
├── internal/              # Go application's core logic
│   ├── database/
│   ├── handler/
│   └── model/
├── py-service/            # Python microservice for OCR and ML
├── uploads/               # Storage for uploaded files and thumbnails
├── web/                   # Frontend templates and components
├── .github/workflows/     # CI/CD workflows
├── Dockerfile             # Dockerfile for the Go application
├── docker-compose.yml     # Production Docker Compose file
└── README.md
``` 