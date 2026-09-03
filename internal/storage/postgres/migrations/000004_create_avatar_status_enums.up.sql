CREATE TYPE upload_status AS ENUM (
    'uploaded',
    'deleted'
);

CREATE TYPE processing_status AS ENUM (
    'pending',
    'processing',
    'completed',
    'failed'
);

ALTER TABLE avatars
    ALTER COLUMN upload_status DROP DEFAULT,
    ALTER COLUMN processing_status DROP DEFAULT;

ALTER TABLE avatars
    ALTER COLUMN upload_status TYPE upload_status
    USING upload_status::text::upload_status;

ALTER TABLE avatars
    ALTER COLUMN processing_status TYPE processing_status
    USING processing_status::text::processing_status;

ALTER TABLE avatars
    ALTER COLUMN upload_status SET DEFAULT 'uploaded'::upload_status,
    ALTER COLUMN processing_status SET DEFAULT 'pending'::processing_status;

ALTER TABLE avatars
    DROP CONSTRAINT avatars_upload_status_check,
    DROP CONSTRAINT avatars_processing_status_check;