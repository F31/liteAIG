-- owner: catalog
-- Audio model catalog: speech-to-text/translation models declare the "audio"
-- capability; text-to-speech models declare "speech" so the routing hard
-- constraint can distinguish the two directions (spec §5.1 P1 "audio").
INSERT INTO resource_model_catalog(id, provider_id, model, capabilities, context_window, sort_order) VALUES
('openai-whisper-1', 'openai', 'whisper-1', '["audio"]', NULL, 100),
('openai-tts-1', 'openai', 'tts-1', '["speech"]', NULL, 110),
('openai-tts-1-hd', 'openai', 'tts-1-hd', '["speech"]', NULL, 120),
('openai-gpt-4o-mini-tts', 'openai', 'gpt-4o-mini-tts', '["speech"]', NULL, 130)
ON CONFLICT(id) DO NOTHING;
