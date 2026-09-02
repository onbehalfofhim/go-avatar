CREATE TABLE avatars (
    id UUID PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL,
    file_name VARCHAR(255) NOT NULL,
    mime_type VARCHAR(100) NOT NULL,
    size_bytes BIGINT NOT NULL,
    s3_key VARCHAR(1024) NOT NULL,
    thumbnail_s3_keys JSONB NOT NULL DEFAULT '{}'::jsonb,
    upload_status VARCHAR(20) NOT NULL DEFAULT 'uploaded',
    processing_status VARCHAR(20) NOT NULL DEFAULT 'pending',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,

    CONSTRAINT avatars_size_bytes_check
        CHECK (size_bytes > 0),

    CONSTRAINT avatars_upload_status_check
        CHECK (upload_status IN ('uploaded', 'deleted')),

    CONSTRAINT avatars_processing_status_check
        CHECK (
            processing_status IN (
                'pending',
                'processing',
                'completed',
                'failed'
            )
        )
);

CREATE INDEX idx_avatars_user_id
    ON avatars (user_id);

CREATE INDEX idx_avatars_created_at
    ON avatars (created_at);

CREATE INDEX idx_avatars_deleted_at
    ON avatars (deleted_at);

CREATE UNIQUE INDEX idx_avatars_one_active_per_user
    ON avatars (user_id)
    WHERE is_active = TRUE AND deleted_at IS NULL;