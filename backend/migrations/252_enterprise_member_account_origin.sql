-- Enterprise password recovery is restricted to accounts created for an enterprise.
ALTER TABLE enterprise_members
    ADD COLUMN IF NOT EXISTS account_created_by_enterprise BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE enterprise_members m
SET account_created_by_enterprise = TRUE
FROM users u
WHERE u.id = m.user_id
  AND ABS(EXTRACT(EPOCH FROM (u.created_at - m.created_at))) <= 10;
