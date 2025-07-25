# Dokeep: The Document Library - Project Blueprint

This document outlines the plan for creating "dokeep", a web application for storing, organizing, and searching digitalized documents.

## 1. Core Concepts

- **Application Name**: dokeep
- **Main Goal**: To store and organize digitalized documents (PDF, JPG, PNG).
- **Key Features**:
    - User authentication with username/password and TOTP.
    - User-specific document storage on a dedicated dashboard.
    - Document upload with thumbnail generation for images and PDFs.
    - OCR (Optical Character Recognition) to extract text from documents.
    - Tagging for organization.
    - Full-text search of document content.
    - Search by metadata (title, tags, upload date).

## 2. Technology Stack

- **Backend**: Go
- **Frontend**: HTML rendered server-side.
- **Templating**: `templ` for type-safe HTML templates in Go.
- **Styling**: TailwindCSS via the Play CDN for rapid UI development.
- **Database**: PostgreSQL, managed via Docker.
- **Authentication**: We'll use a library like `github.com/pquerna/otp` for TOTP and `scs` for session management.

## 3. Architecture

We will use a microservices-based architecture.

- **Go Service**: The main web application, responsible for user authentication, document management, and serving the UI.
- **Python Service**: A sidecar service responsible for CPU-intensive tasks. It uses FastAPI for the web server and leverages the following for its machine learning pipeline:
    - **Thumbnail Generation**: `Pillow` and `pdf2image`.
    - **OCR**: `Pytesseract`.
    - **Automated Tagging**: A Naive Bayes classifier (`MultinomialNB`) with a TF-IDF vectorizer from `scikit-learn`, and a sophisticated NLP pre-processing pipeline using `SpaCy` for lemmatization and stop-word removal.

## 4. Asynchronous Document Processing Pipeline (New Architecture)

To improve upload performance and system robustness, we are moving to an asynchronous, queue-based architecture for document processing.

-   **Upload Process**:
    1.  The user uploads a document via the Go application.
    2.  The Go application immediately creates a new entry in the `documents` table with a `status` of `queued` and saves the original file.
    3.  The Go application then forwards the file to a new endpoint on the Python service (`py-service`).
    4.  The Python service saves the file to a dedicated processing queue directory (`uploads/queue/`).
    5.  The Go application immediately returns a response to the user, directing them to a new "Queue" page.

-   **Background Worker (Python Service)**:
    1.  A background worker in the `py-service` constantly monitors the `uploads/queue/` directory.
    2.  When a new file appears, the worker picks it up for processing.
    3.  It updates the document's `status` in the database to `processing`.
    4.  It performs all the intensive tasks: OCR, thumbnail generation, and calling the `llm-service` for advanced analysis.
    5.  Upon completion, the worker directly updates the document's record in the database with the extracted content, summary, tags, and thumbnail path. The `status` is set to `completed`.
    6.  If an error occurs, the `status` is set to `failed`, and the error message is logged in the database.

-   **Queue UI (Go Application)**:
    1.  A new `/queue` page is added to the Go application.
    2.  This page lists all documents that are not yet in `completed` status.
    3.  It will automatically refresh to show the real-time status of each document in the queue.

-   **Direct Database Access**:
    -   The `py-service` will be granted direct access to the PostgreSQL database to update the status and results of the document processing, making it a more independent and capable part of the system.

## 5. Project Structure

A suggested directory structure to keep the project organized:

```
dokeep/
├── cmd/
│   └── dokeep/
│       └── main.go         # Application entry point
├── internal/
│   ├── auth/               # Authentication and session logic
│   ├── config/             # Configuration loading
│   ├── database/           # Database setup and queries
│   ├── handler/            # HTTP handlers
│   ├── middleware/         # HTTP middleware
│   ├── model/              # Data models (structs)
│   └── ocr/                # OCR processing logic
├── py-service/
│   ├── main.py             # Python service entry point
│   └── requirements.txt
├── uploads/
│   ├── [files]             # Original uploaded files
│   └── thumbnails/         # Generated thumbnails
├── web/
│   ├── template/           # Templ files for the UI
│   │   ├── layout.templ
│   │   ├── index.templ
│   │   ├── dashboard.templ
│   │   └── document.templ
│   └── static/             # Static assets (if any in the future)
├── go.mod                  # Go module definition
├── go.sum
└── plan.md                 # This file
```

## 6. Database Schema

We'll use a PostgreSQL database with the following schema.

**`users` table:**

| Column             | Type        | Constraints      | Description                               |
|--------------------|-------------|------------------|-------------------------------------------|
| `id`               | SERIAL      | PRIMARY KEY      | Unique identifier for the user.           |
| `username`         | TEXT        | NOT NULL, UNIQUE | User's chosen username.                   |
| `password_hash`    | TEXT        | NOT NULL         | Hashed password.                          |
| `totp_secret`      | TEXT        |                  | Secret key for TOTP.                      |
| `totp_enabled`     | BOOLEAN     | DEFAULT FALSE    | Whether TOTP is enabled for the user.     |

**`documents` table:**

| Column        | Type        | Constraints                      | Description                               |
|---------------|-------------|----------------------------------|-------------------------------------------|
| `id`          | SERIAL      | PRIMARY KEY                      | Unique identifier for the document.       |
| `user_id`     | INTEGER     | REFERENCES users(id)             | Foreign key to the `users` table.         |
| `title`       | TEXT        | NOT NULL                         | User-defined title for the document.      |
| `file_path`   | TEXT        | NOT NULL                         | Path to the original file on the server.  |
| `thumbnail`   | TEXT        |                                  | Path to the generated thumbnail file.     |
| `content`     | TEXT        |                                  | OCR-extracted text content.               |
| `created_at`  | TIMESTAMPTZ | DEFAULT CURRENT_TIMESTAMP        | Timestamp of when the document was uploaded.|

**`tags` table:**

| Column | Type    | Constraints       | Description                    |
|--------|---------|-------------------|--------------------------------|
| `id`   | SERIAL  | PRIMARY KEY       | Unique identifier for the tag. |
| `name` | TEXT    | NOT NULL, UNIQUE  | The name of the tag (e.g., "invoices"). |

**`document_tags` table (Junction Table):**

| Column         | Type    | Constraints                             | Description                                |
|----------------|---------|-----------------------------------------|--------------------------------------------|
| `document_id`  | INTEGER | REFERENCES documents(id) ON DELETE CASCADE | Foreign key to the `documents` table.      |
| `tag_id`       | INTEGER | REFERENCES tags(id) ON DELETE CASCADE   | Foreign key to the `tags` table.           |
|                |         | PRIMARY KEY (`document_id`, `tag_id`)       | Ensures unique document-tag pairings.      |

## 6. Development Milestones

We will build the application in phases.

### Milestone 1: Project Setup & Basic Server
- [x] Initialize the Go module (`go mod init dokeep`).
- [x] Set up the directory structure as outlined above.
- [x] Create a basic HTTP server in `main.go`.
- [x] Integrate `templ` and serve a simple "Hello, World!" page using a layout and index template.
- [x] Include the TailwindCSS Play CDN link in the main layout.

### Milestone 2: User Authentication
- [x] Set up the SQLite database and create the `users` table.
- [x] Implement user registration and login pages and handlers.
- [x] Hash passwords using a secure algorithm (e.g., bcrypt).
- [x] Implement session management (e.g., using cookies).
- [x] Implement TOTP setup and validation.
- [x] Create middleware to protect routes that require authentication.
- [x] Separate the public landing page from the authenticated user dashboard.

### Milestone 3: Document Upload and Listing
- [x] Create the rest of the database schema (`documents`, `tags`, `document_tags`).
- [x] Implement the file upload form and handler for authenticated users on the dashboard.
- [x] On upload, save the file to a designated folder with a unique name to prevent overwrites.
- [x] Store document metadata (title, file path, `user_id`) in the `documents` table.
- [x] Display a list of documents owned by the logged-in user on the dashboard.
- [x] Outsource thumbnail generation for images and PDFs to the Python service.

### Milestone 4: OCR Integration
- [x] Outsource OCR processing to the Python service.
- [x] Modify the upload handler to call the OCR service for all file types.
- [x] Save the extracted text into the `content` column of the `documents` table.
- [x] Create a document detail page to display the extracted content.

### Milestone 5: Tagging System
- [x] Create the UI for adding and viewing tags on a document page.
- [x] Implement backend logic for manual tagging.
- [x] Create a machine learning service in Python for automated tagging, optimized for low-resource environments.
- [x] Implement an NLP pipeline with SpaCy for advanced text processing.
- [x] Implement a training endpoint to train a Naive Bayes model.
- [x] Implement a prediction endpoint to suggest tags for new documents.

### Milestone 6: Search Functionality
- [x] Implement a search handler that queries the database for the current user's documents.
- [x] The search matches text in the `title`, `content`, and `tags` fields.
- [x] Add a search bar to the UI to allow users to perform searches.

### Milestone 7: Refinements & Polish
- [x] Overhaul the UI with a utility-focused and responsive design.
- [x] Implement a new application layout with a header and side menu.
- [x] Redesign the public-facing pages (Home, Login, Register).
- [x] Redesign the authenticated pages (Dashboard, Document View).
- [x] Create a new "User Settings" page for password and TOTP management.
- [x] Implement modals for document upload, tag management, and training confirmation.
- [x] Add pagination for the document list on the dashboard.
- [x] Add a gallery view for documents with a view toggle.
- [x] Add proper error handling and logging.
- [ ] Write tests for critical components. 