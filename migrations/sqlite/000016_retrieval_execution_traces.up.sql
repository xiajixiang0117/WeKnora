CREATE TABLE retrieval_execution_traces (
    id TEXT PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    session_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    user_message_id TEXT NOT NULL DEFAULT '',
    assistant_message_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    trace TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, request_id)
);

CREATE INDEX idx_retrieval_execution_traces_tenant_session_created
    ON retrieval_execution_traces (tenant_id, session_id, created_at);
