ALTER TABLE jobs ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS max_attempts INTEGER NOT NULL DEFAULT 3;
ALTER TABLE jobs ADD CONSTRAINT jobs_attempts_nonnegative CHECK (attempts >= 0);
ALTER TABLE jobs ADD CONSTRAINT jobs_max_attempts_positive CHECK (max_attempts > 0);