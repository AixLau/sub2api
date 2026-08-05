-- Dynamic API merchant SSO integrations, user bindings, and recharge records.

CREATE TABLE IF NOT EXISTS merchant_integrations (
    id              BIGSERIAL PRIMARY KEY,
    name            VARCHAR(120) NOT NULL,
    code            VARCHAR(100) NOT NULL,
    mode            VARCHAR(32) NOT NULL DEFAULT 'dynamic_api',
    merchant_code   VARCHAR(120) NOT NULL DEFAULT '',
    description     VARCHAR(500) NOT NULL DEFAULT '',
    status          VARCHAR(20) NOT NULL DEFAULT 'draft',
    enabled         BOOLEAN NOT NULL DEFAULT FALSE,
    redirect_hosts  JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT merchant_integrations_name_check CHECK (BTRIM(name) <> ''),
    CONSTRAINT merchant_integrations_code_check CHECK (BTRIM(code) <> ''),
    CONSTRAINT merchant_integrations_mode_check CHECK (mode = 'dynamic_api'),
    CONSTRAINT merchant_integrations_status_check CHECK (status IN ('draft', 'active', 'disabled')),
    CONSTRAINT merchant_integrations_redirect_hosts_check CHECK (jsonb_typeof(redirect_hosts) = 'array'),
    CONSTRAINT merchant_integrations_code_unique UNIQUE (code)
);

CREATE INDEX IF NOT EXISTS idx_merchant_integrations_status_enabled
    ON merchant_integrations (status, enabled);

CREATE TABLE IF NOT EXISTS merchant_api_endpoints (
    id                BIGSERIAL PRIMARY KEY,
    integration_id     BIGINT NOT NULL,
    type              VARCHAR(32) NOT NULL,
    url               VARCHAR(2048) NOT NULL,
    method            VARCHAR(10) NOT NULL DEFAULT 'POST',
    content_type      VARCHAR(80) NOT NULL DEFAULT 'application/json',
    query_template    JSONB NOT NULL DEFAULT '{}'::jsonb,
    header_template   JSONB NOT NULL DEFAULT '{}'::jsonb,
    body_template     JSONB NOT NULL DEFAULT '{}'::jsonb,
    auth_type         VARCHAR(20) NOT NULL DEFAULT 'none',
    secret_ref        VARCHAR(255) NOT NULL DEFAULT '',
    response_mapping  JSONB NOT NULL DEFAULT '{}'::jsonb,
    success_rule      JSONB NOT NULL DEFAULT '{}'::jsonb,
    retry_policy      JSONB NOT NULL DEFAULT '{"maxAttempts":1,"backoffMs":300}'::jsonb,
    timeout_ms        INT NOT NULL DEFAULT 10000,
    status            VARCHAR(20) NOT NULL DEFAULT 'active',
    enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT merchant_api_endpoints_integration_fk
        FOREIGN KEY (integration_id) REFERENCES merchant_integrations(id) ON DELETE CASCADE,
    CONSTRAINT merchant_api_endpoints_type_check CHECK (
        type IN ('register_login', 'register', 'login', 'token', 'sync', 'bind', 'status', 'callback', 'recharge_records')
    ),
    CONSTRAINT merchant_api_endpoints_method_check CHECK (method IN ('GET', 'POST', 'PUT', 'PATCH', 'DELETE')),
    CONSTRAINT merchant_api_endpoints_content_type_check CHECK (
        content_type IN ('application/json', 'application/x-www-form-urlencoded')
    ),
    CONSTRAINT merchant_api_endpoints_auth_type_check CHECK (auth_type IN ('none', 'api_key', 'bearer', 'basic', 'hmac')),
    CONSTRAINT merchant_api_endpoints_status_check CHECK (status IN ('draft', 'active', 'disabled')),
    CONSTRAINT merchant_api_endpoints_timeout_check CHECK (timeout_ms BETWEEN 100 AND 120000),
    CONSTRAINT merchant_api_endpoints_query_check CHECK (jsonb_typeof(query_template) = 'object'),
    CONSTRAINT merchant_api_endpoints_header_check CHECK (jsonb_typeof(header_template) = 'object'),
    CONSTRAINT merchant_api_endpoints_body_check CHECK (jsonb_typeof(body_template) = 'object'),
    CONSTRAINT merchant_api_endpoints_mapping_check CHECK (jsonb_typeof(response_mapping) = 'object'),
    CONSTRAINT merchant_api_endpoints_success_rule_check CHECK (jsonb_typeof(success_rule) = 'object'),
    CONSTRAINT merchant_api_endpoints_retry_check CHECK (jsonb_typeof(retry_policy) = 'object'),
    CONSTRAINT merchant_api_endpoints_type_unique UNIQUE (integration_id, type)
);

CREATE INDEX IF NOT EXISTS idx_merchant_api_endpoints_state
    ON merchant_api_endpoints (integration_id, enabled, status);

CREATE TABLE IF NOT EXISTS merchant_bindings (
    id                    BIGSERIAL PRIMARY KEY,
    integration_id        BIGINT NOT NULL,
    user_id               BIGINT NOT NULL,
    external_user_id      VARCHAR(255) NOT NULL,
    external_account      VARCHAR(255) NOT NULL DEFAULT '',
    status                VARCHAR(20) NOT NULL DEFAULT 'active',
    last_login_at         TIMESTAMPTZ,
    last_sync_at          TIMESTAMPTZ,
    last_recharge_sync_at TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT merchant_bindings_integration_fk
        FOREIGN KEY (integration_id) REFERENCES merchant_integrations(id) ON DELETE CASCADE,
    CONSTRAINT merchant_bindings_user_fk
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT merchant_bindings_external_user_check CHECK (BTRIM(external_user_id) <> ''),
    CONSTRAINT merchant_bindings_status_check CHECK (status IN ('active', 'disabled')),
    CONSTRAINT merchant_bindings_user_integration_unique UNIQUE (integration_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_merchant_bindings_external_user
    ON merchant_bindings (integration_id, external_user_id);
CREATE INDEX IF NOT EXISTS idx_merchant_bindings_user_status
    ON merchant_bindings (user_id, status);

CREATE TABLE IF NOT EXISTS merchant_recharge_records (
    id                  BIGSERIAL PRIMARY KEY,
    integration_id      BIGINT NOT NULL,
    user_id             BIGINT NOT NULL,
    external_user_id    VARCHAR(255) NOT NULL DEFAULT '',
    order_no            VARCHAR(128) NOT NULL,
    amount              VARCHAR(64) NOT NULL DEFAULT '',
    currency            VARCHAR(16) NOT NULL DEFAULT '',
    balance_before      VARCHAR(64) NOT NULL DEFAULT '',
    balance_after       VARCHAR(64) NOT NULL DEFAULT '',
    charge_type         VARCHAR(32) NOT NULL DEFAULT '',
    pay_method          VARCHAR(32) NOT NULL DEFAULT '',
    status              VARCHAR(32) NOT NULL DEFAULT '',
    platform_order_no   VARCHAR(128) NOT NULL DEFAULT '',
    merchant_created_at TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT merchant_recharge_records_integration_fk
        FOREIGN KEY (integration_id) REFERENCES merchant_integrations(id) ON DELETE CASCADE,
    CONSTRAINT merchant_recharge_records_user_fk
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT merchant_recharge_records_order_check CHECK (BTRIM(order_no) <> ''),
    CONSTRAINT merchant_recharge_records_dedup_unique
        UNIQUE (integration_id, user_id, order_no, merchant_created_at)
);

CREATE INDEX IF NOT EXISTS idx_merchant_recharge_records_user_integration_time
    ON merchant_recharge_records (user_id, integration_id, merchant_created_at DESC);
