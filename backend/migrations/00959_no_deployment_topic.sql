-- +goose Up
-- Nothing publishes ci.deployment.succeeded, so it is no longer a topic an
-- endpoint can take (#417). An endpoint that named it never received it, and
-- keeping the name would refuse that endpoint's next edit.

UPDATE webhook_endpoint SET topics = array_remove(topics, 'ci.deployment.succeeded')
WHERE 'ci.deployment.succeeded' = ANY (topics);

-- +goose Down
-- Which endpoints named it is not kept, and none of them missed anything.
SELECT 1;
