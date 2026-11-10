-- One vocabulary (spec 0027, AC-5): a running stack is a stack, and Compose's project name keeps
-- its own column. Renames only, so every row, link and digest stays as it is. This file names the
-- old names on purpose. vocabulary: allow

-- +goose Up
ALTER TABLE deployments RENAME TO stacks; -- vocabulary: allow
ALTER TABLE stacks RENAME COLUMN project TO compose_project;
ALTER TABLE operations RENAME COLUMN deployment_id TO stack_id; -- vocabulary: allow
ALTER TABLE operations RENAME COLUMN plan_digest TO digest; -- vocabulary: allow
ALTER INDEX operations_deployment_started_idx RENAME TO operations_stack_started_idx; -- vocabulary: allow
ALTER TABLE stacks RENAME CONSTRAINT deployments_pkey TO stacks_pkey; -- vocabulary: allow
ALTER TABLE stacks RENAME CONSTRAINT deployments_name_key TO stacks_name_key; -- vocabulary: allow
ALTER TABLE stacks RENAME CONSTRAINT deployments_project_key TO stacks_compose_project_key; -- vocabulary: allow
ALTER TABLE stacks RENAME CONSTRAINT deployments_name_check TO stacks_name_check; -- vocabulary: allow
ALTER TABLE stacks RENAME CONSTRAINT deployments_latest_operation_fk TO stacks_latest_operation_fk; -- vocabulary: allow
ALTER TABLE operations RENAME CONSTRAINT operations_deployment_id_fkey TO operations_stack_id_fkey; -- vocabulary: allow

-- +goose Down
ALTER TABLE operations RENAME CONSTRAINT operations_stack_id_fkey TO operations_deployment_id_fkey; -- vocabulary: allow
ALTER TABLE stacks RENAME CONSTRAINT stacks_latest_operation_fk TO deployments_latest_operation_fk; -- vocabulary: allow
ALTER TABLE stacks RENAME CONSTRAINT stacks_name_check TO deployments_name_check; -- vocabulary: allow
ALTER TABLE stacks RENAME CONSTRAINT stacks_compose_project_key TO deployments_project_key; -- vocabulary: allow
ALTER TABLE stacks RENAME CONSTRAINT stacks_name_key TO deployments_name_key; -- vocabulary: allow
ALTER TABLE stacks RENAME CONSTRAINT stacks_pkey TO deployments_pkey; -- vocabulary: allow
ALTER INDEX operations_stack_started_idx RENAME TO operations_deployment_started_idx; -- vocabulary: allow
ALTER TABLE operations RENAME COLUMN digest TO plan_digest; -- vocabulary: allow
ALTER TABLE operations RENAME COLUMN stack_id TO deployment_id; -- vocabulary: allow
ALTER TABLE stacks RENAME COLUMN compose_project TO project;
ALTER TABLE stacks RENAME TO deployments; -- vocabulary: allow
