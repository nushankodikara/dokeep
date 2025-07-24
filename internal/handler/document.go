package handler

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dokeep/internal/model"
	"dokeep/web/template"
	"log"

	"mime/multipart"

	"github.com/alexedwards/scs/v2"
)

type DocumentHandler struct {
	DB      *sql.DB
	Session *scs.SessionManager
}

type OcrResult struct {
	Content       string `json:"text"`
	ThumbnailPath string `json:"thumbnail_path"`
	ExtractedDate string `json:"extracted_date"`
	FileHash      string `json:"file_hash"`
}

type LlmAnalysisResult struct {
	ExtractedDate string   `json:"extracted_date"`
	Tags          []string `json:"tags"`
	Summary       string   `json:"summary"`
}

func (h *DocumentHandler) List(w http.ResponseWriter, r *http.Request) ([]model.Document, int, error) {
	userID := h.Session.GetInt(r.Context(), "userID")
	query := r.URL.Query().Get("q")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit := 10
	offset := (page - 1) * limit

	var documents []model.Document
	var totalDocs int
	var args []interface{}
	var countArgs []interface{}

	sqlQuery := `
		SELECT DISTINCT d.id, d.title, d.file_path, d.thumbnail, d.content, d.summary, d.created_date, d.created_at
		FROM documents d
		LEFT JOIN document_tags dt ON d.id = dt.document_id
		LEFT JOIN tags t ON dt.tag_id = t.id
		WHERE d.user_id = ?
	`
	countQuery := `
		SELECT COUNT(DISTINCT d.id)
		FROM documents d
		LEFT JOIN document_tags dt ON d.id = dt.document_id
		LEFT JOIN tags t ON dt.tag_id = t.id
		WHERE d.user_id = ?
	`
	args = append(args, userID)
	countArgs = append(countArgs, userID)

	if query != "" {
		searchCondition := `
			AND (
				d.title LIKE ? OR
				d.content LIKE ? OR
				d.summary LIKE ? OR
				t.name LIKE ?
			)
		`
		sqlQuery += searchCondition
		countQuery += searchCondition
		likeQuery := "%" + query + "%"
		args = append(args, likeQuery, likeQuery, likeQuery, likeQuery)
		countArgs = append(countArgs, likeQuery, likeQuery, likeQuery, likeQuery)
	}

	sqlQuery += " ORDER BY d.created_date DESC, d.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	// Get total count for pagination
	err := h.DB.QueryRow(countQuery, countArgs...).Scan(&totalDocs)
	if err != nil {
		return nil, 0, err
	}

	rows, err := h.DB.Query(sqlQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var doc model.Document
		var createdDate sql.NullTime
		if err := rows.Scan(&doc.ID, &doc.Title, &doc.FilePath, &doc.Thumbnail, &doc.Content, &doc.Summary, &createdDate, &doc.CreatedAt); err != nil {
			return nil, 0, err
		}
		if createdDate.Valid {
			doc.CreatedDate = createdDate.Time
		}
		documents = append(documents, doc)
	}

	return documents, totalDocs, nil
}

func (h *DocumentHandler) GetTags(documentID int) ([]model.Tag, error) {
	rows, err := h.DB.Query(`
		SELECT t.id, t.name
		FROM tags t
		JOIN document_tags dt ON t.id = dt.tag_id
		WHERE dt.document_id = ?
	`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []model.Tag
	for rows.Next() {
		var tag model.Tag
		if err := rows.Scan(&tag.ID, &tag.Name); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

func (h *DocumentHandler) addTagsToDocument(docID int, tags []string) {
	for _, tagName := range tags {
		// Normalize the tag: trim whitespace and convert to lowercase
		normalizedTag := strings.TrimSpace(strings.ToLower(tagName))
		if normalizedTag == "" {
			continue // Skip empty tags
		}

		// Check if tag exists, otherwise create it
		var tagID int
		err := h.DB.QueryRow("SELECT id FROM tags WHERE name = ?", normalizedTag).Scan(&tagID)
		if err == sql.ErrNoRows {
			res, err := h.DB.Exec("INSERT INTO tags (name) VALUES (?)", normalizedTag)
			if err != nil {
				log.Printf("Error inserting new tag '%s': %v", normalizedTag, err)
				continue
			}
			id, _ := res.LastInsertId()
			tagID = int(id)
		} else if err != nil {
			log.Printf("Error querying for tag '%s': %v", normalizedTag, err)
			continue
		}

		// Associate tag with document
		_, err = h.DB.Exec("INSERT OR IGNORE INTO document_tags (document_id, tag_id) VALUES (?, ?)", docID, tagID)
		if err != nil {
			log.Printf("Error associating tag '%s' with document %d: %v", normalizedTag, docID, err)
		}
	}
}

func (h *DocumentHandler) AddTag(w http.ResponseWriter, r *http.Request) {
	documentID, err := strconv.Atoi(r.URL.Path[len("/document/") : len(r.URL.Path)-len("/tags")])
	if err != nil {
		http.Error(w, "Invalid document ID", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}
	tagName := r.FormValue("tag")
	if tagName == "" {
		http.Redirect(w, r, fmt.Sprintf("/document?id=%d", documentID), http.StatusSeeOther)
		return
	}
	h.addTagsToDocument(documentID, []string{tagName})
	http.Redirect(w, r, fmt.Sprintf("/document?id=%d", documentID), http.StatusSeeOther)
}

func (h *DocumentHandler) Show(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil {
		http.Error(w, "Invalid document ID", http.StatusBadRequest)
		return
	}

	userID := h.Session.GetInt(r.Context(), "userID")

	var doc model.Document
	var createdDate sql.NullTime
	err = h.DB.QueryRow("SELECT id, title, file_path, thumbnail, content, summary, created_date, created_at FROM documents WHERE id = ? AND user_id = ?", id, userID).Scan(&doc.ID, &doc.Title, &doc.FilePath, &doc.Thumbnail, &doc.Content, &doc.Summary, &createdDate, &doc.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Document not found", http.StatusNotFound)
		} else {
			http.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}
	if createdDate.Valid {
		doc.CreatedDate = createdDate.Time
	}

	tags, err := h.GetTags(id)
	if err != nil {
		log.Printf("Error getting tags for document %d: %v", id, err)
		// Non-fatal, we can still render the page
	}

	if err := template.DocumentPage(doc.Title, doc, tags).Render(r.Context(), w); err != nil {
		http.Error(w, "Error rendering document page", http.StatusInternalServerError)
	}
}

func (h *DocumentHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "Error parsing multipart form", http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	userSummary := r.FormValue("summary")
	createdDateStr := r.FormValue("created_date")
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error retrieving the file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Ensure uploads directory exists
	uploadDir := filepath.Join(".", "uploads")
	if err := os.MkdirAll(uploadDir, os.ModePerm); err != nil {
		http.Error(w, "Unable to create uploads directory", http.StatusInternalServerError)
		return
	}

	// Create a temporary file
	tempFile, err := os.CreateTemp(uploadDir, "upload-*.tmp")
	if err != nil {
		http.Error(w, "Could not create temporary file", http.StatusInternalServerError)
		return
	}
	io.Copy(tempFile, file)
	tempFile.Close() // Close the file so the python service can access it.

	// Create a unique filename to avoid overwriting files
	ext := filepath.Ext(header.Filename)
	baseName := strings.TrimSuffix(header.Filename, ext)
	newFileName := fmt.Sprintf("%s-%d%s", baseName, time.Now().Unix(), ext)
	filePath := filepath.Join(uploadDir, newFileName)

	if err := os.Rename(tempFile.Name(), filePath); err != nil {
		http.Error(w, "Could not rename temporary file", http.StatusInternalServerError)
		return
	}

	// Call Python service for OCR and thumbnail
	ocrResult, err := h.callOcrService(filePath)
	if err != nil {
		log.Printf("Error from OCR service: %v", err)
		// Important: Clean up the saved file if the microservice fails
		os.Remove(filePath)
		http.Error(w, "Failed to process document with external service. The document has not been saved.", http.StatusInternalServerError)
		return
	}

	initialTags, err := h.callPredictService(ocrResult.Content)
	if err != nil {
		log.Printf("Prediction service failed: %v", err)
		initialTags = []string{} // Default to empty list on error
	}

	llmResult, err := h.callLlmService(ocrResult.Content, initialTags)
	if err != nil {
		log.Printf("LLM analysis failed: %v. Proceeding without LLM data.", err)
		// Non-fatal, so we'll just log and continue, but use initial tags as a fallback
		llmResult = &LlmAnalysisResult{Tags: initialTags}
	}

	// Use user's summary if provided, otherwise fall back to LLM's summary
	finalSummary := userSummary
	if finalSummary == "" {
		finalSummary = llmResult.Summary
	}

	var createdDate time.Time
	// Priority: User > LLM > OCR > Now
	// First, try the user-provided date
	if createdDateStr != "" {
		parsedDate, err := time.Parse("2006-01-02", createdDateStr)
		if err == nil {
			createdDate = parsedDate
		}
	}

	// If the user didn't provide a valid date, try the extracted date from LLM
	if createdDate.IsZero() && llmResult.ExtractedDate != "" {
		// LLM's extracted date is in a specific format, e.g., "YYYY-MM-DDTHH:MM:SS"
		const layout = "2006-01-02T15:04:05"
		parsedDate, err := time.Parse(layout, llmResult.ExtractedDate)
		if err == nil {
			createdDate = parsedDate
		} else {
			log.Printf("Could not parse LLM extracted date '%s': %v. Defaulting to current time.", llmResult.ExtractedDate, err)
		}
	}

	// If we still don't have a date, try the extracted date from OCR
	if createdDate.IsZero() && ocrResult.ExtractedDate != "" {
		// Python's isoformat on a naive datetime is like "YYYY-MM-DDTHH:MM:SS".
		// We need to parse this specific format, not the full RFC3339 with timezone.
		const layout = "2006-01-02T15:04:05"
		parsedDate, err := time.Parse(layout, ocrResult.ExtractedDate)
		if err == nil {
			createdDate = parsedDate
		} else {
			log.Printf("Could not parse extracted date '%s': %v. Defaulting to current time.", ocrResult.ExtractedDate, err)
		}
	}

	// If we still don't have a date, default to now
	if createdDate.IsZero() {
		createdDate = time.Now()
	}

	userID := h.Session.GetInt(r.Context(), "userID")

	// Check for duplicate hash for this user
	var existingID int
	err = h.DB.QueryRow("SELECT id FROM documents WHERE user_id = ? AND file_hash = ?", userID, ocrResult.FileHash).Scan(&existingID)
	if err != nil && err != sql.ErrNoRows {
		// Real database error
		os.Remove(filePath) // Clean up
		http.Error(w, "Database error during duplicate check", http.StatusInternalServerError)
		return
	}
	if existingID > 0 {
		// Duplicate found
		os.Remove(filePath) // Clean up the new file
		log.Printf("Duplicate file upload blocked for user %d. Hash: %s", userID, ocrResult.FileHash)
		h.Session.Put(r.Context(), "flash_error", "This file has already been uploaded.")
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	// Now that everything is successful, save the document to the database
	res, err := h.DB.Exec("INSERT INTO documents (user_id, title, file_path, content, thumbnail, summary, created_date, file_hash) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		userID, title, filePath, ocrResult.Content, ocrResult.ThumbnailPath, finalSummary, createdDate, ocrResult.FileHash)
	if err != nil {
		log.Printf("Error saving document to database: %v", err)
		// Clean up files if DB insert fails
		os.Remove(filePath)
		if ocrResult.ThumbnailPath != "" {
			os.Remove(ocrResult.ThumbnailPath)
		}
		http.Error(w, "Error saving document to database", http.StatusInternalServerError)
		return
	}

	docID, _ := res.LastInsertId()

	// Add final tags from LLM (or the initial tags if LLM failed)
	if len(llmResult.Tags) > 0 {
		h.addTagsToDocument(int(docID), llmResult.Tags)
	}

	log.Printf("Successfully uploaded and processed document ID: %d", docID)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *DocumentHandler) Train(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
		SELECT d.content, t.name
		FROM documents d
		JOIN document_tags dt ON d.id = dt.document_id
		JOIN tags t ON dt.tag_id = t.id
		WHERE d.content != ""
	`)
	if err != nil {
		http.Error(w, "Failed to fetch training data", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	docMap := make(map[string][]string)
	for rows.Next() {
		var content, tagName string
		if err := rows.Scan(&content, &tagName); err != nil {
			continue
		}
		docMap[content] = append(docMap[content], tagName)
	}

	var documents []string
	var tags [][]string
	for doc, tagList := range docMap {
		documents = append(documents, doc)
		tags = append(tags, tagList)
	}

	trainingData := map[string]interface{}{
		"documents": documents,
		"tags":      tags,
	}

	body := &bytes.Buffer{}
	json.NewEncoder(body).Encode(trainingData)
	req, _ := http.NewRequest("POST", "http://localhost:8000/train", body)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		http.Error(w, "Failed to train model", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	w.Write([]byte("Training initiated successfully!"))
}

func (h *DocumentHandler) RemoveTag(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/document/"), "/tags/")
	if len(parts) != 2 {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}
	documentID, err := strconv.Atoi(parts[0])
	if err != nil {
		http.Error(w, "Invalid document ID", http.StatusBadRequest)
		return
	}
	tagName := parts[1]

	// Normalize the tag name before searching for it
	normalizedTag := strings.TrimSpace(strings.ToLower(tagName))

	// Get tag ID
	var tagID int
	err = h.DB.QueryRow("SELECT id FROM tags WHERE name = ?", normalizedTag).Scan(&tagID)
	if err != nil {
		log.Printf("Attempted to remove non-existent tag '%s' (normalized: '%s')", tagName, normalizedTag)
		http.Error(w, "Tag not found", http.StatusNotFound)
		return
	}

	// Remove association
	_, err = h.DB.Exec("DELETE FROM document_tags WHERE document_id = ? AND tag_id = ?", documentID, tagID)
	if err != nil {
		http.Error(w, "Failed to remove tag association", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/document?id=%d", documentID), http.StatusSeeOther)
}

func (h *DocumentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 {
		log.Println("Delete handler: Invalid URL path:", r.URL.Path)
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	documentID, err := strconv.Atoi(parts[1])
	if err != nil {
		log.Println("Delete handler: Invalid document ID:", parts[1])
		http.Error(w, "Invalid document ID", http.StatusBadRequest)
		return
	}

	userID := h.Session.GetInt(r.Context(), "userID")

	// First, verify the user owns the document and get the file paths
	var filePath, thumbnailPath string
	err = h.DB.QueryRow("SELECT file_path, thumbnail FROM documents WHERE id = ? AND user_id = ?", documentID, userID).Scan(&filePath, &thumbnailPath)
	if err != nil {
		log.Printf("Delete handler: Document not found or access denied for doc %d and user %d. Error: %v", documentID, userID, err)
		http.Error(w, "Document not found or access denied", http.StatusNotFound)
		return
	}

	// Delete the document record from the database
	_, err = h.DB.Exec("DELETE FROM documents WHERE id = ?", documentID)
	if err != nil {
		log.Printf("Delete handler: Failed to delete document %d from database. Error: %v", documentID, err)
		http.Error(w, "Failed to delete document from database", http.StatusInternalServerError)
		return
	}

	// Also delete the associated tags
	_, err = h.DB.Exec("DELETE FROM document_tags WHERE document_id = ?", documentID)
	if err != nil {
		log.Printf("Delete handler: Failed to delete tags for document %d. Error: %v", documentID, err)
		// Log this error, but don't block the user
	}

	// Delete the actual files from the filesystem
	if filePath != "" {
		if err := os.Remove(filePath); err != nil {
			log.Printf("Delete handler: Failed to remove file %s. Error: %v", filePath, err)
		}
	}
	if thumbnailPath != "" {
		if err := os.Remove(thumbnailPath); err != nil {
			log.Printf("Delete handler: Failed to remove thumbnail %s. Error: %v", thumbnailPath, err)
		}
	}

	log.Printf("Delete handler: Successfully deleted document %d", documentID)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *DocumentHandler) UpdateDate(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		log.Println("UpdateDate handler: Invalid URL path:", r.URL.Path)
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	documentID, err := strconv.Atoi(parts[1])
	if err != nil {
		log.Println("UpdateDate handler: Invalid document ID:", parts[1])
		http.Error(w, "Invalid document ID", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}
	createdDateStr := r.FormValue("created_date")
	createdDate, err := time.Parse("2006-01-02", createdDateStr)
	if err != nil {
		log.Println("UpdateDate handler: Invalid date format:", createdDateStr)
		http.Error(w, "Invalid date format", http.StatusBadRequest)
		return
	}

	userID := h.Session.GetInt(r.Context(), "userID")
	_, err = h.DB.Exec("UPDATE documents SET created_date = ? WHERE id = ? AND user_id = ?", createdDate, documentID, userID)
	if err != nil {
		log.Printf("UpdateDate handler: Failed to update date for doc %d and user %d. Error: %v", documentID, userID, err)
		http.Error(w, "Failed to update document date", http.StatusInternalServerError)
		return
	}

	log.Printf("UpdateDate handler: Successfully updated date for document %d", documentID)
	http.Redirect(w, r, fmt.Sprintf("/document?id=%d", documentID), http.StatusSeeOther)
}

func (h *DocumentHandler) UpdateDetails(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}
	documentID, err := strconv.Atoi(parts[1])
	if err != nil {
		http.Error(w, "Invalid document ID", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	summary := r.FormValue("summary")
	createdDateStr := r.FormValue("created_date")
	createdDate, err := time.Parse("2006-01-02", createdDateStr)
	if err != nil {
		http.Error(w, "Invalid date format", http.StatusBadRequest)
		return
	}

	userID := h.Session.GetInt(r.Context(), "userID")
	_, err = h.DB.Exec("UPDATE documents SET title = ?, summary = ?, created_date = ? WHERE id = ? AND user_id = ?",
		title, summary, createdDate, documentID, userID)
	if err != nil {
		http.Error(w, "Failed to update document details", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/document?id=%d", documentID), http.StatusSeeOther)
}

func (h *DocumentHandler) callOcrService(filePath string) (*OcrResult, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open file for ocr service: %w", err)
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("could not create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("could not copy file to form: %w", err)
	}
	writer.Close()

	// Determine the service URL based on the environment
	// In Docker, the service is reachable by its name. Locally, it's localhost.
	serviceURL := "http://dokeep-service:8000/process"
	if os.Getenv("DOKEEP_ENV") != "docker" {
		serviceURL = "http://localhost:8000/process"
	}

	req, err := http.NewRequest("POST", serviceURL, body)
	if err != nil {
		return nil, fmt.Errorf("could not create request to ocr service: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ocr service request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ocr service returned non-OK status: %s - %s", resp.Status, string(respBody))
	}

	var result OcrResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("could not decode ocr service response: %w", err)
	}

	return &result, nil
}

func (h *DocumentHandler) callPredictService(content string) ([]string, error) {
	serviceURL := "http://dokeep-service:8000/predict"
	if os.Getenv("DOKEEP_ENV") != "docker" {
		serviceURL = "http://localhost:8000/predict"
	}

	requestBody, err := json.Marshal(map[string]string{"document": content})
	if err != nil {
		return nil, fmt.Errorf("could not marshal predict request: %w", err)
	}

	req, err := http.NewRequest("POST", serviceURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("could not create request to predict service: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("predict service request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("predict service returned non-OK status: %s", resp.Status)
	}

	var result map[string][]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("could not decode predict service response: %w", err)
	}
	return result["tags"], nil
}

func (h *DocumentHandler) callLlmService(content string, initialTags []string) (*LlmAnalysisResult, error) {
	serviceURL := "http://llm-service:8001/analyze"
	if os.Getenv("DOKEEP_ENV") != "docker" {
		serviceURL = "http://localhost:8001/analyze"
	}

	requestBody, err := json.Marshal(map[string]interface{}{
		"content":      content,
		"initial_tags": initialTags,
	})
	if err != nil {
		return nil, fmt.Errorf("could not marshal llm request: %w", err)
	}

	req, err := http.NewRequest("POST", serviceURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("could not create request to llm service: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Minute} // Generous timeout for LLM
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm service request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm service returned non-OK status: %s", resp.Status)
	}

	var result LlmAnalysisResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("could not decode llm service response: %w", err)
	}
	return &result, nil
}

func (h *DocumentHandler) analyzeAndSaveContent(documentID int64, content string, initialTags []string) {
	// Check if AI features are disabled
	if os.Getenv("DISABLE_AI") == "1" {
		log.Println("AI features are disabled. Skipping content analysis.")
		return
	}

	serviceURL := "http://llm-service:8001/analyze"

	requestBody, err := json.Marshal(map[string]interface{}{
		"content":      content,
		"initial_tags": initialTags,
	})
	if err != nil {
		log.Printf("Error marshalling request for document %d: %v", documentID, err)
		return
	}

	req, err := http.NewRequest("POST", serviceURL, bytes.NewBuffer(requestBody))
	if err != nil {
		log.Printf("Error creating request for document %d: %v", documentID, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error sending request for document %d: %v", documentID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Non-OK status for document %d: %s", documentID, resp.Status)
		return
	}

	var result LlmAnalysisResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Error decoding response for document %d: %v", documentID, err)
		return
	}

	// Save the results to the database
	_, err = h.DB.Exec("UPDATE documents SET summary = ?, created_date = ? WHERE id = ?",
		result.Summary, result.ExtractedDate, documentID)
	if err != nil {
		log.Printf("Error updating document %d: %v", documentID, err)
		return
	}

	// Add the tags to the document
	h.addTagsToDocument(int(documentID), result.Tags)
}
