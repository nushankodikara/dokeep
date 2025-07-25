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

	// Base query components
	baseSelect := "SELECT DISTINCT d.id, d.title, d.file_path, d.thumbnail, d.content, d.summary, d.created_date, d.created_at"
	baseFrom := "FROM documents d LEFT JOIN document_tags dt ON d.id = dt.document_id LEFT JOIN tags t ON dt.tag_id = t.id"
	countSelect := "SELECT COUNT(DISTINCT d.id)"

	// Dynamic WHERE clause
	whereClauses := []string{"d.user_id = $1"}
	args := []interface{}{userID}

	if query != "" {
		likeQuery := "%" + query + "%"
		searchCondition := fmt.Sprintf(`(
			d.title ILIKE $%d OR
			d.content ILIKE $%d OR
			d.summary ILIKE $%d OR
			t.name ILIKE $%d
		)`, len(args)+1, len(args)+2, len(args)+3, len(args)+4)
		whereClauses = append(whereClauses, searchCondition)
		args = append(args, likeQuery, likeQuery, likeQuery, likeQuery)
	}

	fullWhere := ""
	if len(whereClauses) > 0 {
		fullWhere = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Get total count for pagination first
	countQuery := countSelect + " " + baseFrom + " " + fullWhere
	err := h.DB.QueryRow(countQuery, args...).Scan(&totalDocs)
	if err != nil {
		return nil, 0, err
	}

	// Now, build the final query for the documents
	orderBy := fmt.Sprintf("ORDER BY d.created_date DESC, d.created_at DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	sqlQuery := baseSelect + " " + baseFrom + " " + fullWhere + " " + orderBy

	rows, err := h.DB.Query(sqlQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var doc model.Document
		var createdDate sql.NullTime
		var content, summary, filePath, thumbnail sql.NullString
		if err := rows.Scan(&doc.ID, &doc.Title, &filePath, &thumbnail, &content, &summary, &createdDate, &doc.CreatedAt); err != nil {
			return nil, 0, err
		}
		if createdDate.Valid {
			doc.CreatedDate = createdDate.Time
		}
		doc.Content = content.String
		doc.Summary = summary.String
		doc.FilePath = filePath.String
		doc.Thumbnail = thumbnail.String
		documents = append(documents, doc)
	}

	return documents, totalDocs, nil
}

func (h *DocumentHandler) GetTags(documentID int) ([]model.Tag, error) {
	rows, err := h.DB.Query(`
		SELECT t.id, t.name
		FROM tags t
		JOIN document_tags dt ON t.id = dt.tag_id
		WHERE dt.document_id = $1
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
		err := h.DB.QueryRow("SELECT id FROM tags WHERE name = $1", normalizedTag).Scan(&tagID)
		if err == sql.ErrNoRows {
			err = h.DB.QueryRow("INSERT INTO tags (name) VALUES ($1) RETURNING id", normalizedTag).Scan(&tagID)
			if err != nil {
				log.Printf("Error inserting new tag '%s': %v", normalizedTag, err)
				continue
			}
		} else if err != nil {
			log.Printf("Error querying for tag '%s': %v", normalizedTag, err)
			continue
		}

		// Associate tag with document
		_, err = h.DB.Exec("INSERT INTO document_tags (document_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", docID, tagID)
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
	var content, summary, filePath, thumbnail sql.NullString
	err = h.DB.QueryRow("SELECT id, title, file_path, thumbnail, content, summary, created_date, created_at FROM documents WHERE id = $1 AND user_id = $2", id, userID).Scan(&doc.ID, &doc.Title, &filePath, &thumbnail, &content, &summary, &createdDate, &doc.CreatedAt)
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
	doc.Content = content.String
	doc.Summary = summary.String
	doc.FilePath = filePath.String
	doc.Thumbnail = thumbnail.String

	tags, err := h.GetTags(id)
	if err != nil {
		log.Printf("Error getting tags for document %d: %v", id, err)
		// Non-fatal, we can still render the page
	}

	if err := template.DocumentPage(doc.Title, doc, tags).Render(r.Context(), w); err != nil {
		http.Error(w, "Error rendering document page", http.StatusInternalServerError)
	}
}

func (h *DocumentHandler) Queue(w http.ResponseWriter, r *http.Request) {
	userID := h.Session.GetInt(r.Context(), "userID")
	username := h.Session.GetString(r.Context(), "username")

	rows, err := h.DB.Query("SELECT id, title, original_filename, status, status_message FROM documents WHERE user_id = $1 AND status != 'completed' ORDER BY created_at ASC", userID)
	if err != nil {
		http.Error(w, "Failed to retrieve queued documents", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var documents []model.Document
	for rows.Next() {
		var doc model.Document
		var statusMessage, originalFilename sql.NullString
		if err := rows.Scan(&doc.ID, &doc.Title, &originalFilename, &doc.Status, &statusMessage); err != nil {
			log.Printf("Error scanning queued document: %v", err)
			continue
		}
		if statusMessage.Valid {
			doc.StatusMessage = statusMessage.String
		}
		if originalFilename.Valid {
			doc.OriginalFilename = originalFilename.String
		}
		documents = append(documents, doc)
	}

	if err := template.QueuePage(username, documents).Render(r.Context(), w); err != nil {
		http.Error(w, "Error rendering queue page", http.StatusInternalServerError)
	}
}

func (h *DocumentHandler) QueueStatus(w http.ResponseWriter, r *http.Request) {
	userID := h.Session.GetInt(r.Context(), "userID")

	rows, err := h.DB.Query("SELECT id, title, original_filename, status, status_message FROM documents WHERE user_id = $1 AND status != 'completed' ORDER BY created_at ASC", userID)
	if err != nil {
		// We don't write an error to the response here because this is for polling.
		// A client-side error will be logged.
		log.Printf("Error retrieving queue status for user %d: %v", userID, err)
		return
	}
	defer rows.Close()

	var documents []model.Document
	for rows.Next() {
		var doc model.Document
		var statusMessage, originalFilename sql.NullString
		if err := rows.Scan(&doc.ID, &doc.Title, &originalFilename, &doc.Status, &statusMessage); err != nil {
			log.Printf("Error scanning queued document for status update: %v", err)
			continue
		}
		if statusMessage.Valid {
			doc.StatusMessage = statusMessage.String
		}
		if originalFilename.Valid {
			doc.OriginalFilename = originalFilename.String
		}
		documents = append(documents, doc)
	}

	// Render only the rows, not the whole page
	for _, doc := range documents {
		template.QueueRow(doc).Render(r.Context(), w)
	}
}

func (h *DocumentHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "Error parsing multipart form", http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error retrieving the file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	userID := h.Session.GetInt(r.Context(), "userID")

	// 1. Save a record to the database first to get an ID
	var docID int64
	err = h.DB.QueryRow("INSERT INTO documents (user_id, title, original_filename, file_path) VALUES ($1, $2, $3, $4) RETURNING id",
		userID, title, header.Filename, "").Scan(&docID)
	if err != nil {
		log.Printf("Error creating initial document record: %v", err)
		http.Error(w, "Could not create document record", http.StatusInternalServerError)
		return
	}

	// 2. Save the file to a permanent location with a unique name based on the ID
	uploadDir := filepath.Join(".", "uploads")
	if err := os.MkdirAll(uploadDir, os.ModePerm); err != nil {
		http.Error(w, "Unable to create uploads directory", http.StatusInternalServerError)
		return
	}

	ext := filepath.Ext(header.Filename)
	newFileName := fmt.Sprintf("%d%s", docID, ext)
	filePath := filepath.Join(uploadDir, newFileName)

	savedFile, err := os.Create(filePath)
	if err != nil {
		http.Error(w, "Could not save uploaded file", http.StatusInternalServerError)
		return
	}
	defer savedFile.Close()

	// Reset file pointer and copy to the new file
	file.Seek(0, io.SeekStart)
	if _, err := io.Copy(savedFile, file); err != nil {
		http.Error(w, "Could not copy file content", http.StatusInternalServerError)
		return
	}

	// 3. Update the file_path in the database
	_, err = h.DB.Exec("UPDATE documents SET file_path = $1 WHERE id = $2", filePath, docID)
	if err != nil {
		log.Printf("Error updating file path for document %d: %v", docID, err)
		os.Remove(filePath) // Cleanup
		http.Error(w, "Could not update document record", http.StatusInternalServerError)
		return
	}

	// 4. Call the Python service to queue the file for processing
	if err := h.callProcessService(filePath, docID); err != nil {
		log.Printf("Error calling process service for document %d: %v", docID, err)
		os.Remove(filePath) // Cleanup
		// Also delete the DB record
		h.DB.Exec("DELETE FROM documents WHERE id = $1", docID)
		http.Error(w, "Failed to queue document for processing.", http.StatusInternalServerError)
		return
	}

	// 5. Redirect to the queue page
	http.Redirect(w, r, "/queue", http.StatusSeeOther)
}

// callProcessService sends the file to the Python service to be queued.
func (h *DocumentHandler) callProcessService(filePath string, docID int64) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("could not open file for processing service: %w", err)
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add the doc_id as a form field
	if err := writer.WriteField("doc_id", strconv.FormatInt(docID, 10)); err != nil {
		return fmt.Errorf("could not write doc_id field: %w", err)
	}

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return fmt.Errorf("could not create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("could not copy file to form: %w", err)
	}
	writer.Close()

	serviceURL := "http://dokeep-service:8000/process"
	if os.Getenv("DOKEEP_ENV") != "docker" {
		serviceURL = "http://localhost:8000/process"
	}

	req, err := http.NewRequest("POST", serviceURL, body)
	if err != nil {
		return fmt.Errorf("could not create request to process service: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 1 * time.Minute} // 1 minute timeout should be plenty
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("process service request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("process service returned non-OK status: %s - %s", resp.Status, string(respBody))
	}

	return nil
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
	err = h.DB.QueryRow("SELECT id FROM tags WHERE name = $1", normalizedTag).Scan(&tagID)
	if err != nil {
		log.Printf("Attempted to remove non-existent tag '%s' (normalized: '%s')", tagName, normalizedTag)
		http.Error(w, "Tag not found", http.StatusNotFound)
		return
	}

	// Remove association
	_, err = h.DB.Exec("DELETE FROM document_tags WHERE document_id = $1 AND tag_id = $2", documentID, tagID)
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
	var filePath, thumbnailPath sql.NullString
	err = h.DB.QueryRow("SELECT file_path, thumbnail FROM documents WHERE id = $1 AND user_id = $2", documentID, userID).Scan(&filePath, &thumbnailPath)
	if err != nil {
		log.Printf("Delete handler: Document not found or access denied for doc %d and user %d. Error: %v", documentID, userID, err)
		http.Error(w, "Document not found or access denied", http.StatusNotFound)
		return
	}

	// Delete the document record from the database
	_, err = h.DB.Exec("DELETE FROM documents WHERE id = $1", documentID)
	if err != nil {
		log.Printf("Delete handler: Failed to delete document %d from database. Error: %v", documentID, err)
		http.Error(w, "Failed to delete document from database", http.StatusInternalServerError)
		return
	}

	// Also delete the associated tags in the same transaction
	_, err = h.DB.Exec("DELETE FROM document_tags WHERE document_id = $1", documentID)
	if err != nil {
		log.Printf("Delete handler: Failed to delete tags for document %d. Error: %v", documentID, err)
		// Log this error, but don't block the user
	}

	// Delete the actual files from the filesystem
	if filePath.Valid {
		if err := os.Remove(filePath.String); err != nil {
			log.Printf("Delete handler: Failed to remove file %s. Error: %v", filePath.String, err)
		}
	}
	if thumbnailPath.Valid {
		if err := os.Remove(thumbnailPath.String); err != nil {
			log.Printf("Delete handler: Failed to remove thumbnail %s. Error: %v", thumbnailPath.String, err)
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
	_, err = h.DB.Exec("UPDATE documents SET created_date = $1 WHERE id = $2 AND user_id = $3", createdDate, documentID, userID)
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
	_, err = h.DB.Exec("UPDATE documents SET title = $1, summary = $2, created_date = $3 WHERE id = $4 AND user_id = $5",
		title, summary, createdDate, documentID, userID)
	if err != nil {
		http.Error(w, "Failed to update document details", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/document?id=%d", documentID), http.StatusSeeOther)
}
