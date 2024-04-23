package connectors

import "time"

type Chunk struct {
	Text       string
	SourceURL  string
	SourceName string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
