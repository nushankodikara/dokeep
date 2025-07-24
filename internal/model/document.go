package model

import "time"

type Document struct {
	ID          int
	Title       string
	FilePath    string
	Thumbnail   string
	Content     string
	Summary     string
	FileHash    string
	CreatedDate time.Time
	CreatedAt   time.Time
}
