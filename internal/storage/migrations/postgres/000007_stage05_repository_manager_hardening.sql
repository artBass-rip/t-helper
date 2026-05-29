-- +goose Up
UPDATE repositories r
SET provider_instance_id = NULL
WHERE provider_instance_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM repository_provider_instances p WHERE p.id = r.provider_instance_id
  );

UPDATE repositories r
SET default_credential_id = NULL
WHERE default_credential_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM repository_credentials c WHERE c.id = r.default_credential_id
  );

ALTER TABLE repositories
  ADD CONSTRAINT repositories_provider_instance_id_fkey
  FOREIGN KEY (provider_instance_id)
  REFERENCES repository_provider_instances(id)
  ON DELETE SET NULL
  NOT VALID;

ALTER TABLE repositories
  VALIDATE CONSTRAINT repositories_provider_instance_id_fkey;

ALTER TABLE repositories
  ADD CONSTRAINT repositories_default_credential_id_fkey
  FOREIGN KEY (default_credential_id)
  REFERENCES repository_credentials(id)
  ON DELETE SET NULL
  NOT VALID;

ALTER TABLE repositories
  VALIDATE CONSTRAINT repositories_default_credential_id_fkey;

CREATE INDEX IF NOT EXISTS repository_credentials_provider_instance_auth_type_idx
  ON repository_credentials (provider_instance_id, auth_type);
CREATE INDEX IF NOT EXISTS repositories_root_target_directory_idx
  ON repositories (root_path_id, target_directory);
CREATE INDEX IF NOT EXISTS repositories_root_local_path_idx
  ON repositories (root_path_id, local_path);

-- +goose Down
DROP INDEX IF EXISTS repositories_root_local_path_idx;
DROP INDEX IF EXISTS repositories_root_target_directory_idx;
DROP INDEX IF EXISTS repository_credentials_provider_instance_auth_type_idx;
ALTER TABLE repositories DROP CONSTRAINT IF EXISTS repositories_default_credential_id_fkey;
ALTER TABLE repositories DROP CONSTRAINT IF EXISTS repositories_provider_instance_id_fkey;
