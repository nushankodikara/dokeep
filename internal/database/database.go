package database

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func InitDB(dataSourceName string) *sql.DB {
	db, err := sql.Open("sqlite3", dataSourceName)
	if err != nil {
		log.Fatalf("could not connect to database: %v", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatalf("could not ping database: %v", err)
	}

	createUsersTableSQL := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
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
		data BLOB NOT NULL,
		expiry REAL NOT NULL
	);`

	if _, err := db.Exec(createSessionsTableSQL); err != nil {
		log.Fatalf("could not create sessions table: %v", err)
	}

	createDocumentsTableSQL := `
	CREATE TABLE IF NOT EXISTS documents (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER,
		title TEXT NOT NULL,
		file_path TEXT NOT NULL,
		content TEXT,
		thumbnail TEXT,
		summary TEXT,
		file_hash TEXT,
		created_date DATE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id),
		UNIQUE (user_id, file_hash)
	);`

	if _, err := db.Exec(createDocumentsTableSQL); err != nil {
		log.Fatalf("could not create documents table: %v", err)
	}

	createTagsTableSQL := `
	CREATE TABLE IF NOT EXISTS tags (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE
	);`

	if _, err := db.Exec(createTagsTableSQL); err != nil {
		log.Fatalf("could not create tags table: %v", err)
	}

	createDocumentTagsTableSQL := `
	CREATE TABLE IF NOT EXISTS document_tags (
		document_id INTEGER,
		tag_id INTEGER,
		PRIMARY KEY (document_id, tag_id),
		FOREIGN KEY (document_id) REFERENCES documents(id),
		FOREIGN KEY (tag_id) REFERENCES tags(id)
	);`

	if _, err := db.Exec(createDocumentTagsTableSQL); err != nil {
		log.Fatalf("could not create document_tags table: %v", err)
	}

	return db
}
