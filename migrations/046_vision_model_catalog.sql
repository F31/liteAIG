-- owner: catalog
-- Correct the OpenAI vision-capable catalog entries so the setup wizard's
-- catalog-driven capability resolution marks image-bearing models correctly.
UPDATE resource_model_catalog
SET capabilities = '["chat","vision"]'
WHERE id = 'openai-gpt-4o-mini';
