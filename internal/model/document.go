package model

import "time"

type Document struct {
	ID               int
	Title            string
	OriginalFilename string
	FilePath         string
	Thumbnail        string
	Content          string
	Summary          string
	FileHash         string
	Status           string
	StatusMessage    string
	CreatedDate      time.Time
	CreatedAt        time.Time
}
