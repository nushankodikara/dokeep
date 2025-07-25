package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
)

func InitDB() *sql.DB {
	host := os.Getenv("DB_HOST")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")

	connStr := fmt.Sprintf("host=%s user=%s password=%s dbname=%s sslmode=disable",
		host, user, password, dbname)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("could not connect to database: %v", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatalf("could not ping database: %v", err)
	}

	createUsersTableSQL := `
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		totp_secret TEXT,
		totp_enabled BOOLEAN DEFAULT FALSE
	);`

	if _, err := db.Exec(createUsersTableSQL); err != nil {
		log.Fatalf("could not create users table: %v", err)
	}

	createSessionsTableSQL := `
	CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		data BYTEA NOT NULL,
		expiry TIMESTAMPTZ NOT NULL
	);`

	if _, err := db.Exec(createSessionsTableSQL); err != nil {
		log.Fatalf("could not create sessions table: %v", err)
	}

	createDocumentsTableSQL := `
	CREATE TABLE IF NOT EXISTS documents (
		id SERIAL PRIMARY KEY,
		user_id INTEGER REFERENCES users(id),
		title TEXT NOT NULL,
		original_filename TEXT,
		file_path TEXT NOT NULL,
		content TEXT,
		thumbnail TEXT,
		summary TEXT,
		file_hash TEXT,
		status TEXT NOT NULL DEFAULT 'queued',
		status_message TEXT,
		created_date DATE,
		created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (user_id, file_hash)
	);`

	if _, err := db.Exec(createDocumentsTableSQL); err != nil {
		log.Fatalf("could not create documents table: %v", err)
	}

	createTagsTableSQL := `
	CREATE TABLE IF NOT EXISTS tags (
		id SERIAL PRIMARY KEY,
		name TEXT NOT NULL UNIQUE
	);`

	if _, err := db.Exec(createTagsTableSQL); err != nil {
		log.Fatalf("could not create tags table: %v", err)
	}

	createDocumentTagsTableSQL := `
	CREATE TABLE IF NOT EXISTS document_tags (
		document_id INTEGER REFERENCES documents(id) ON DELETE CASCADE,
		tag_id INTEGER REFERENCES tags(id) ON DELETE CASCADE,
		PRIMARY KEY (document_id, tag_id)
	);`

	if _, err := db.Exec(createDocumentTagsTableSQL); err != nil {
		log.Fatalf("could not create document_tags table: %v", err)
	}

	return db
}
