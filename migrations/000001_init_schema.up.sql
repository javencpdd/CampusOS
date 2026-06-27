-- CampusOS 初始化数据库 Schema
-- 所有表使用 BIGINT 主键（雪花算法生成）
-- 所有表包含 created_at、updated_at、deleted_at 审计字段
-- 禁止物理删除，使用软删除

-- ═══════════════════════════════════════
-- 身份域 (Identity Domain)
-- ═══════════════════════════════════════

-- 用户表
CREATE TABLE IF NOT EXISTS users (
    id          BIGINT PRIMARY KEY,
    username    VARCHAR(32) NOT NULL,
    nickname    VARCHAR(64) NOT NULL,
    email       VARCHAR(255) NOT NULL,
    avatar      VARCHAR(512) DEFAULT '',
    bio         VARCHAR(500) DEFAULT '',
    status      VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_users_username ON users(username) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uk_users_email ON users(email) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_users_status ON users(status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_users_created_at ON users(created_at DESC) WHERE deleted_at IS NULL;

-- 账号表（密码等凭据）
CREATE TABLE IF NOT EXISTS accounts (
    id          BIGINT PRIMARY KEY,
    user_id     BIGINT NOT NULL,
    type        VARCHAR(20) NOT NULL,       -- email / phone / oauth
    identifier  VARCHAR(255) NOT NULL,      -- 邮箱/手机号
    credential  VARCHAR(512) NOT NULL,      -- 密码哈希
    verified    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_accounts_type_identifier ON accounts(type, identifier) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_accounts_user_id ON accounts(user_id) WHERE deleted_at IS NULL;

-- 会话表
CREATE TABLE IF NOT EXISTS sessions (
    id              BIGINT PRIMARY KEY,
    user_id         BIGINT NOT NULL,
    refresh_token   VARCHAR(255) NOT NULL,
    device_id       VARCHAR(128) DEFAULT '',
    device_name     VARCHAR(128) DEFAULT '',
    device_type     VARCHAR(20) DEFAULT 'web',
    ip_address      VARCHAR(45) DEFAULT '',
    user_agent      VARCHAR(512) DEFAULT '',
    last_active_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMP NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_sessions_refresh_token ON sessions(refresh_token) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id) WHERE deleted_at IS NULL;

-- ═══════════════════════════════════════
-- 社区域 (Community Domain)
-- ═══════════════════════════════════════

-- 版块表
CREATE TABLE IF NOT EXISTS categories (
    id            BIGINT PRIMARY KEY,
    name          VARCHAR(64) NOT NULL,
    slug          VARCHAR(64) NOT NULL,
    description   VARCHAR(500) DEFAULT '',
    icon          VARCHAR(512) DEFAULT '',
    parent_id     BIGINT NULL,
    sort_order    INTEGER NOT NULL DEFAULT 0,
    thread_count  BIGINT NOT NULL DEFAULT 0,
    post_count    BIGINT NOT NULL DEFAULT 0,
    is_closed     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_categories_slug ON categories(slug) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_categories_parent_id ON categories(parent_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_categories_sort_order ON categories(sort_order) WHERE deleted_at IS NULL;

-- 帖子表
CREATE TABLE IF NOT EXISTS threads (
    id              BIGINT PRIMARY KEY,
    title           VARCHAR(255) NOT NULL,
    content         TEXT NOT NULL,
    content_format  VARCHAR(20) NOT NULL DEFAULT 'markdown',
    author_id       BIGINT NOT NULL,
    author_name     VARCHAR(64) NOT NULL DEFAULT '',
    category_id     BIGINT NOT NULL,
    status          VARCHAR(20) NOT NULL DEFAULT 'published',
    is_pinned       BOOLEAN NOT NULL DEFAULT FALSE,
    is_locked       BOOLEAN NOT NULL DEFAULT FALSE,
    is_highlighted  BOOLEAN NOT NULL DEFAULT FALSE,
    view_count      BIGINT NOT NULL DEFAULT 0,
    reply_count     BIGINT NOT NULL DEFAULT 0,
    like_count      BIGINT NOT NULL DEFAULT 0,
    last_post_id    BIGINT NULL,
    last_post_at    TIMESTAMP NULL,
    tags            TEXT[] DEFAULT '{}',
    metadata        JSONB DEFAULT '{}',
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMP NULL
);

CREATE INDEX IF NOT EXISTS idx_threads_author_id ON threads(author_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_threads_category_id ON threads(category_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_threads_status ON threads(status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_threads_is_pinned ON threads(is_pinned) WHERE deleted_at IS NULL AND is_pinned = TRUE;
CREATE INDEX IF NOT EXISTS idx_threads_created_at ON threads(created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_threads_last_post_at ON threads(last_post_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_threads_tags ON threads USING GIN(tags) WHERE deleted_at IS NULL;

-- 回复表
CREATE TABLE IF NOT EXISTS posts (
    id              BIGINT PRIMARY KEY,
    thread_id       BIGINT NOT NULL,
    author_id       BIGINT NOT NULL,
    author_name     VARCHAR(64) NOT NULL DEFAULT '',
    parent_id       BIGINT NULL,
    content         TEXT NOT NULL,
    content_format  VARCHAR(20) NOT NULL DEFAULT 'markdown',
    status          VARCHAR(20) NOT NULL DEFAULT 'published',
    like_count      BIGINT NOT NULL DEFAULT 0,
    floor_number    INTEGER NOT NULL DEFAULT 0,
    metadata        JSONB DEFAULT '{}',
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMP NULL
);

CREATE INDEX IF NOT EXISTS idx_posts_thread_id ON posts(thread_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_posts_author_id ON posts(author_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_posts_parent_id ON posts(parent_id) WHERE deleted_at IS NULL AND parent_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_posts_created_at ON posts(created_at) WHERE deleted_at IS NULL;

-- 标签表
CREATE TABLE IF NOT EXISTS tags (
    id           BIGINT PRIMARY KEY,
    name         VARCHAR(32) NOT NULL,
    slug         VARCHAR(64) NOT NULL,
    description  VARCHAR(255) DEFAULT '',
    color        VARCHAR(7) DEFAULT '#007bff',
    thread_count BIGINT NOT NULL DEFAULT 0,
    created_at   TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at   TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_tags_name ON tags(name) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uk_tags_slug ON tags(slug) WHERE deleted_at IS NULL;

-- 点赞表
CREATE TABLE IF NOT EXISTS likes (
    id           BIGINT PRIMARY KEY,
    user_id      BIGINT NOT NULL,
    target_type  VARCHAR(20) NOT NULL,  -- thread / post
    target_id    BIGINT NOT NULL,
    created_at   TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at   TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_likes_user_target ON likes(user_id, target_type, target_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_likes_target ON likes(target_type, target_id) WHERE deleted_at IS NULL;

-- ═══════════════════════════════════════
-- 系统域 (System Domain)
-- ═══════════════════════════════════════

-- 审计日志表（只允许 INSERT，禁止 UPDATE/DELETE）
CREATE TABLE IF NOT EXISTS audit_logs (
    id            BIGINT PRIMARY KEY,
    trace_id      VARCHAR(64) NOT NULL,
    actor_id      BIGINT NULL,
    actor_type    VARCHAR(20) NOT NULL,
    action        VARCHAR(32) NOT NULL,
    resource      VARCHAR(64) NOT NULL,
    resource_id   VARCHAR(64) NOT NULL,
    before_data   JSONB NULL,
    after_data    JSONB NULL,
    metadata      JSONB DEFAULT '{}',
    ip_address    VARCHAR(45) DEFAULT '',
    created_at    TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_id ON audit_logs(actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_resource ON audit_logs(resource, resource_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_trace_id ON audit_logs(trace_id);

-- 通知表
CREATE TABLE IF NOT EXISTS notifications (
    id          BIGINT PRIMARY KEY,
    user_id     BIGINT NOT NULL,
    type        VARCHAR(64) NOT NULL,
    title       VARCHAR(255) NOT NULL,
    content     TEXT DEFAULT '',
    action_url  VARCHAR(512) DEFAULT '',
    is_read     BOOLEAN NOT NULL DEFAULT FALSE,
    read_at     TIMESTAMP NULL,
    metadata    JSONB DEFAULT '{}',
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMP NULL
);

CREATE INDEX IF NOT EXISTS idx_notifications_user_id ON notifications(user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_notifications_user_read ON notifications(user_id, is_read) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_notifications_created_at ON notifications(created_at DESC) WHERE deleted_at IS NULL;

-- 配置表
CREATE TABLE IF NOT EXISTS configurations (
    id          BIGINT PRIMARY KEY,
    key         VARCHAR(255) NOT NULL,
    value       TEXT NOT NULL,
    type        VARCHAR(20) NOT NULL,
    description VARCHAR(500) DEFAULT '',
    category    VARCHAR(64) NOT NULL,
    is_secret   BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by  BIGINT NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMP NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_configurations_key ON configurations(key) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_configurations_category ON configurations(category) WHERE deleted_at IS NULL;
