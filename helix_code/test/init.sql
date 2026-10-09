-- HelixCode Test Database Initialization
-- This script sets up the test database with necessary tables and data

-- Enable required extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    username VARCHAR(255) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(50) DEFAULT 'user',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Sessions table
CREATE TABLE IF NOT EXISTS sessions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    token VARCHAR(255) UNIQUE NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Projects table
CREATE TABLE IF NOT EXISTS projects (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    owner_id UUID REFERENCES users(id) ON DELETE CASCADE,
    status VARCHAR(50) DEFAULT 'active',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Tasks table
CREATE TABLE IF NOT EXISTS tasks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    type VARCHAR(50) NOT NULL,
    status VARCHAR(50) DEFAULT 'pending',
    priority INTEGER DEFAULT 5,
    criticality VARCHAR(50) DEFAULT 'normal',
    assigned_to UUID REFERENCES users(id),
    worker_id UUID,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    max_retries INTEGER DEFAULT 3,
    retry_count INTEGER DEFAULT 0,
    error_message TEXT
);

-- Task dependencies
CREATE TABLE IF NOT EXISTS task_dependencies (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id UUID REFERENCES tasks(id) ON DELETE CASCADE,
    depends_on_task_id UUID REFERENCES tasks(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(task_id, depends_on_task_id)
);

-- Workers table
CREATE TABLE IF NOT EXISTS workers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    hostname VARCHAR(255) UNIQUE NOT NULL,
    display_name VARCHAR(255),
    ssh_config JSONB,
    capabilities TEXT[] DEFAULT '{}',
    resources JSONB DEFAULT '{}',
    status VARCHAR(50) DEFAULT 'offline',
    health_status VARCHAR(50) DEFAULT 'unknown',
    last_heartbeat TIMESTAMP WITH TIME ZONE,
    cpu_usage_percent DECIMAL(5,2) DEFAULT 0,
    memory_usage_percent DECIMAL(5,2) DEFAULT 0,
    disk_usage_percent DECIMAL(5,2) DEFAULT 0,
    current_tasks_count INTEGER DEFAULT 0,
    max_concurrent_tasks INTEGER DEFAULT 5,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Worker metrics
CREATE TABLE IF NOT EXISTS worker_metrics (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    worker_id UUID REFERENCES workers(id) ON DELETE CASCADE,
    cpu_usage_percent DECIMAL(5,2),
    memory_usage_percent DECIMAL(5,2),
    disk_usage_percent DECIMAL(5,2),
    network_rx_bytes BIGINT DEFAULT 0,
    network_tx_bytes BIGINT DEFAULT 0,
    current_tasks_count INTEGER DEFAULT 0,
    temperature_celsius DECIMAL(5,2),
    recorded_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- LLM Models table
CREATE TABLE IF NOT EXISTS llm_models (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    provider VARCHAR(100) NOT NULL,
    model_id VARCHAR(255) NOT NULL,
    capabilities TEXT[] DEFAULT '{}',
    context_length INTEGER,
    status VARCHAR(50) DEFAULT 'active',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- LLM Providers table
CREATE TABLE IF NOT EXISTS llm_providers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) UNIQUE NOT NULL,
    type VARCHAR(50) NOT NULL,
    config JSONB DEFAULT '{}',
    status VARCHAR(50) DEFAULT 'active',
    last_health_check TIMESTAMP WITH TIME ZONE,
    health_status VARCHAR(50) DEFAULT 'unknown',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Notifications table
CREATE TABLE IF NOT EXISTS notifications (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    type VARCHAR(50) NOT NULL,
    title VARCHAR(255) NOT NULL,
    message TEXT,
    channel VARCHAR(50) DEFAULT 'in_app',
    status VARCHAR(50) DEFAULT 'pending',
    priority VARCHAR(20) DEFAULT 'normal',
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    sent_at TIMESTAMP WITH TIME ZONE
);

-- Create indexes for better performance
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_project_id ON tasks(project_id);
CREATE INDEX IF NOT EXISTS idx_tasks_assigned_to ON tasks(assigned_to);
CREATE INDEX IF NOT EXISTS idx_workers_status ON workers(status);
CREATE INDEX IF NOT EXISTS idx_workers_hostname ON workers(hostname);
CREATE INDEX IF NOT EXISTS idx_worker_metrics_worker_id ON worker_metrics(worker_id);
CREATE INDEX IF NOT EXISTS idx_worker_metrics_recorded_at ON worker_metrics(recorded_at);
CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_notifications_user_id ON notifications(user_id);
CREATE INDEX IF NOT EXISTS idx_notifications_status ON notifications(status);

-- Insert test data
INSERT INTO users (username, email, password_hash, role) VALUES
('admin', 'admin@helixcode.test', '$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi', 'admin'),
('developer', 'dev@helixcode.test', '$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi', 'developer'),
('tester', 'test@helixcode.test', '$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi', 'tester')
ON CONFLICT (username) DO NOTHING;

-- Insert test project
INSERT INTO projects (name, description, owner_id) VALUES
('Test Project', 'A test project for HelixCode testing', (SELECT id FROM users WHERE username = 'admin' LIMIT 1))
ON CONFLICT DO NOTHING;

-- Insert test LLM providers
INSERT INTO llm_providers (name, type, config, status) VALUES
('test-llama', 'llamacpp', '{"model_path": "/models/llama-7b.gguf"}', 'active'),
('test-ollama', 'ollama', '{"base_url": "http://ollama:11434"}', 'active')
ON CONFLICT (name) DO NOTHING;

-- Insert test LLM models
INSERT INTO llm_models (name, provider, model_id, capabilities, context_length) VALUES
('Llama 7B Test', 'test-llama', 'llama-7b', ARRAY['text-generation', 'code'], 4096),
('Ollama Test', 'test-ollama', 'llama2:7b', ARRAY['text-generation', 'chat'], 4096)
ON CONFLICT DO NOTHING;

-- Insert test notifications
INSERT INTO notifications (user_id, type, title, message, channel, priority) VALUES
((SELECT id FROM users WHERE username = 'admin' LIMIT 1), 'system', 'Test Notification', 'This is a test notification', 'in_app', 'normal')
ON CONFLICT DO NOTHING;

-- Create test worker entries (will be updated by actual workers)
INSERT INTO workers (hostname, display_name, capabilities, status) VALUES
('worker-1', 'CPU Worker 1', ARRAY['code-generation', 'testing', 'python-execution'], 'offline'),
('worker-2', 'GPU Worker 1', ARRAY['llm-inference', 'model-training', 'cuda-computation'], 'offline'),
('worker-3', 'Memory Worker 1', ARRAY['data-processing', 'analysis', 'docker-execution'], 'offline')
ON CONFLICT (hostname) DO NOTHING;

-- Create MCP servers table
CREATE TABLE IF NOT EXISTS mcp_servers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) UNIQUE NOT NULL,
    transport_type VARCHAR(50) NOT NULL CHECK (transport_type IN ('stdio', 'sse', 'http', 'websocket')),
    command TEXT,
    args TEXT[],
    url TEXT,
    env_vars JSONB NOT NULL DEFAULT '{}',
    status VARCHAR(50) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive', 'failed')),
    last_health_check TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mcp_servers_name ON mcp_servers(name);
CREATE INDEX IF NOT EXISTS idx_mcp_servers_status ON mcp_servers(status);

-- Create tools table
CREATE TABLE IF NOT EXISTS tools (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    mcp_server_id UUID REFERENCES mcp_servers(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    parameters JSONB NOT NULL DEFAULT '{}',
    permissions TEXT[] NOT NULL DEFAULT '{}',
    is_enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tools_mcp_server_id ON tools(mcp_server_id);
CREATE INDEX IF NOT EXISTS idx_tools_name ON tools(name);

-- Create task checkpoints table
CREATE TABLE IF NOT EXISTS task_checkpoints (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id UUID REFERENCES tasks(id) ON DELETE CASCADE,
    checkpoint_name VARCHAR(255) NOT NULL,
    checkpoint_data JSONB NOT NULL,
    worker_id UUID REFERENCES workers(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_checkpoints_task_id ON task_checkpoints(task_id);
CREATE INDEX IF NOT EXISTS idx_task_checkpoints_worker_id ON task_checkpoints(worker_id);

-- Create worker connectivity events table
CREATE TABLE IF NOT EXISTS worker_connectivity_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    worker_id UUID REFERENCES workers(id) ON DELETE CASCADE,
    event_type VARCHAR(50) NOT NULL CHECK (event_type IN ('connected', 'disconnected', 'reconnected', 'heartbeat_missed')),
    event_data JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_worker_connectivity_events_worker_id ON worker_connectivity_events(worker_id);
CREATE INDEX IF NOT EXISTS idx_worker_connectivity_events_event_type ON worker_connectivity_events(event_type);

-- Create audit logs table
CREATE TABLE IF NOT EXISTS audit_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action VARCHAR(255) NOT NULL,
    resource_type VARCHAR(100) NOT NULL,
    resource_id UUID,
    details JSONB NOT NULL DEFAULT '{}',
    ip_address INET,
    user_agent TEXT,
    status VARCHAR(50) NOT NULL CHECK (status IN ('success', 'failure', 'error')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);
CREATE INDEX IF NOT EXISTS idx_audit_logs_resource_type ON audit_logs(resource_type);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at);

-- Update notifications table to match spec
ALTER TABLE notifications
    DROP COLUMN IF EXISTS type,
    ADD COLUMN IF NOT EXISTS notification_type VARCHAR(50) NOT NULL DEFAULT 'info' CHECK (notification_type IN ('info', 'warning', 'error', 'success', 'alert'));

ALTER TABLE notifications
    DROP COLUMN IF EXISTS channel,
    ADD COLUMN IF NOT EXISTS channels TEXT[] DEFAULT '{}';

-- Grant permissions to the configured user.
--
-- This file is mounted into /docker-entrypoint-initdb.d by TWO compose files
-- that create DIFFERENT roles: docker-compose.test.yml uses POSTGRES_USER
-- helixcode, docker-compose.helix.yml uses helix. A hardcoded grantee is
-- therefore wrong in one of them -- naming helixcode literally made the
-- deployment stack abort startup with
--   ERROR: role "helixcode" does not exist
-- and postgres exited 3 before ever accepting a connection.
--
-- Resolve the grantee instead of naming it: init scripts run as POSTGRES_USER,
-- so current_user IS the configured role in either environment. %I quotes the
-- identifier, so a role name needing quoting cannot break the statement.
DO $grant_to_configured_user$
BEGIN
    EXECUTE format('GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO %I', current_user);
    EXECUTE format('GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO %I', current_user);
END
$grant_to_configured_user$;