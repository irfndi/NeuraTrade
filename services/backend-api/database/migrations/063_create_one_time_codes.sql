-- Migration 063: Create one_time_codes table for OTP generation
-- This table stores one-time codes for authentication purposes

CREATE TABLE IF NOT EXISTS one_time_codes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    code VARCHAR(6) NOT NULL,
    purpose VARCHAR(50) NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    used_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT valid_purpose CHECK (purpose IN ('telegram_binding', 'password_reset', 'email_verify'))
);

CREATE INDEX IF NOT EXISTS idx_one_time_codes_user ON one_time_codes(user_id);
CREATE INDEX IF NOT EXISTS idx_one_time_codes_code ON one_time_codes(code, purpose) WHERE used_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_one_time_codes_expires ON one_time_codes(expires_at);
