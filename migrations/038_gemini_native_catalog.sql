-- owner: catalog
UPDATE resource_provider_catalog
SET provider_type = 'gemini', endpoint = 'https://generativelanguage.googleapis.com'
WHERE id = 'google-gemini';

UPDATE resource_model_catalog
SET capabilities = '["chat","vision","stream","tools","reasoning"]'
WHERE id = 'google-gemini-2-5-pro';

UPDATE resource_model_catalog
SET capabilities = '["chat","vision","stream","tools"]'
WHERE id = 'google-gemini-2-5-flash';
