-- owner: catalog
CREATE TABLE resource_provider_catalog (
    id TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    provider_type TEXT NOT NULL,
    endpoint TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE resource_model_catalog (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL REFERENCES resource_provider_catalog(id),
    model TEXT NOT NULL,
    capabilities TEXT NOT NULL DEFAULT '[]',
    context_window INTEGER NULL,
    status TEXT NOT NULL DEFAULT 'active',
    sort_order INTEGER NOT NULL DEFAULT 0,
    UNIQUE(provider_id, model)
);

INSERT INTO resource_provider_catalog(id, label, provider_type, endpoint, sort_order) VALUES
('openai', 'OpenAI', 'openai', 'https://api.openai.com', 10),
('anthropic', 'Anthropic', 'anthropic', 'https://api.anthropic.com', 20),
('deepseek', 'DeepSeek', 'openai-compatible', 'https://api.deepseek.com', 30),
('moonshot', 'Moonshot', 'openai-compatible', 'https://api.moonshot.cn', 40),
('siliconflow', 'SiliconFlow', 'openai-compatible', 'https://api.siliconflow.cn', 50),
('ollama', 'Ollama', 'openai-compatible', 'http://127.0.0.1:11434', 60)
ON CONFLICT(id) DO NOTHING;

INSERT INTO resource_model_catalog(id, provider_id, model, capabilities, context_window, sort_order) VALUES
('openai-gpt-4o-mini', 'openai', 'gpt-4o-mini', '["chat"]', 128000, 10),
('openai-gpt-4o', 'openai', 'gpt-4o', '["chat","vision"]', 128000, 20),
('openai-gpt-4-1', 'openai', 'gpt-4.1', '["chat"]', 1047576, 30),
('anthropic-claude-3-5-sonnet', 'anthropic', 'claude-3-5-sonnet', '["chat"]', 200000, 10),
('anthropic-claude-3-5-haiku', 'anthropic', 'claude-3-5-haiku', '["chat"]', 200000, 20),
('deepseek-v4-flash', 'deepseek', 'deepseek-v4-flash', '["chat"]', 64000, 10),
('deepseek-v4-pro', 'deepseek', 'deepseek-v4-pro', '["chat"]', 64000, 20),
('deepseek-v4-flash-vision-exp', 'deepseek', 'deepseek-v4-flash-vision-exp', '["chat","vision"]', 64000, 30),
('deepseek-chat', 'deepseek', 'deepseek-chat', '["chat"]', 64000, 40),
('deepseek-reasoner', 'deepseek', 'deepseek-reasoner', '["chat","reasoning"]', 64000, 50),
('moonshot-v1-8k', 'moonshot', 'moonshot-v1-8k', '["chat"]', 8192, 10),
('moonshot-v1-32k', 'moonshot', 'moonshot-v1-32k', '["chat"]', 32768, 20),
('moonshot-v1-128k', 'moonshot', 'moonshot-v1-128k', '["chat"]', 131072, 30),
('siliconflow-qwen-2-5-7b-instruct', 'siliconflow', 'Qwen/Qwen2.5-7B-Instruct', '["chat"]', 32768, 10),
('siliconflow-deepseek-v3', 'siliconflow', 'deepseek-ai/DeepSeek-V3', '["chat"]', 64000, 20),
('ollama-llama3-1', 'ollama', 'llama3.1', '["chat"]', 128000, 10),
('ollama-qwen2-5', 'ollama', 'qwen2.5', '["chat"]', 32768, 20),
('ollama-mistral', 'ollama', 'mistral', '["chat"]', 32768, 30)
ON CONFLICT(id) DO NOTHING;
