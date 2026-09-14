CREATE TABLE retrieval_execution_traces (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    session_id VARCHAR(36) NOT NULL,
    request_id VARCHAR(128) NOT NULL,
    user_message_id VARCHAR(36) NOT NULL DEFAULT '',
    assistant_message_id VARCHAR(36) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL,
    trace JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, request_id)
);

CREATE INDEX idx_retrieval_execution_traces_tenant_session_created
    ON retrieval_execution_traces (tenant_id, session_id, created_at);
