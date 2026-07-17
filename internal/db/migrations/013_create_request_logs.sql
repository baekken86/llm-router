-- +goose Up
CREATE TABLE IF NOT EXISTS request_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    log_type TEXT NOT NULL,
    timestamp TIMESTAMP NOT NULL,
    request_id TEXT NOT NULL,
    virtual_model TEXT NOT NULL,
    client_key_id INTEGER,
    provider_name TEXT,
    model_name TEXT,
    status_code INTEGER,
    latency_ms INTEGER,
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    cached_tokens INTEGER DEFAULT 0,
    reasoning_tokens INTEGER DEFAULT 0,
    error_message TEXT,
    retry_count INTEGER DEFAULT 0,
    fallback_count INTEGER DEFAULT 0,
    rtk_intercepted BOOLEAN DEFAULT 0,
    rtk_saved_tokens INTEGER DEFAULT 0,
    caveman_intercepted BOOLEAN DEFAULT 0,
    caveman_saved_tokens INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_request_logs_timestamp ON request_logs(timestamp);
CREATE INDEX IF NOT EXISTS idx_request_logs_virtual_model ON request_logs(virtual_model);
CREATE INDEX IF NOT EXISTS idx_request_logs_type ON request_logs(log_type);
