package domain

import "time"

type UploadStatus string

const (
	UploadStatusUploaded UploadStatus = "uploaded"
	UploadStatusDeleted  UploadStatus = "deleted"
)

type ProcessingStatus string

const (
	ProcessingStatusPending    ProcessingStatus = "pending"
	ProcessingStatusProcessing ProcessingStatus = "processing"
	ProcessingStatusCompleted  ProcessingStatus = "completed"
	ProcessingStatusFailed     ProcessingStatus = "failed"
)

type Avatar struct {
	ID               string
	UserID           string
	FileName         string
	MimeType         string
	SizeBytes        int64
	S3Key            string
	ThumbnailS3Keys  map[string]string
	UploadStatus     UploadStatus
	ProcessingStatus ProcessingStatus
	IsActive         bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}
