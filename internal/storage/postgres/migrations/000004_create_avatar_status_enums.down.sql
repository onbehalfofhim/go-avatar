ALTER TABLE avatars
    ALTER COLUMN upload_status DROP DEFAULT,
    ALTER COLUMN processing_status DROP DEFAULT;

ALTER TABLE avatars
    ALTER COLUMN upload_status TYPE VARCHAR(20)
    USING upload_status::text;

ALTER TABLE avatars
    ALTER COLUMN processing_status TYPE VARCHAR(20)
    USING processing_status::text;

ALTER TABLE avatars
    ALTER COLUMN upload_status SET DEFAULT 'uploaded',
    ALTER COLUMN processing_status SET DEFAULT 'pending';

ALTER TABLE avatars
    ADD CONSTRAINT avatars_upload_status_check
        CHECK (upload_status IN ('uploaded', 'deleted'));

ALTER TABLE avatars
    ADD CONSTRAINT avatars_processing_status_check
        CHECK (
            processing_status IN (
                'pending',
                'processing',
                'completed',
                'failed'
            )
        );

DROP TYPE processing_status;
DROP TYPE upload_status;