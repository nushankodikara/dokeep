package model

// QueueStats holds the counts for various document statuses in the queue.
type QueueStats struct {
	Waiting    int
	Processing int
	Failed     int
}
