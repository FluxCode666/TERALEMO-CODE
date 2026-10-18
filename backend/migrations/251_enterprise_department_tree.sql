-- Department paths include the node itself; roots have depth 1.
ALTER TABLE enterprise_departments
    ADD COLUMN IF NOT EXISTS parent_id BIGINT NULL REFERENCES enterprise_departments(id),
    ADD COLUMN IF NOT EXISTS path TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS depth INT NOT NULL DEFAULT 1;

UPDATE enterprise_departments SET path = '/' || id::text || '/', depth = 1
WHERE path = '' AND parent_id IS NULL;

DROP INDEX IF EXISTS idx_enterprise_departments_name_active;
CREATE UNIQUE INDEX IF NOT EXISTS idx_enterprise_departments_sibling_name_active
    ON enterprise_departments (enterprise_id, parent_id, name)
    WHERE deleted_at IS NULL AND parent_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_enterprise_departments_root_name_active
    ON enterprise_departments (enterprise_id, name)
    WHERE deleted_at IS NULL AND parent_id IS NULL;
CREATE INDEX IF NOT EXISTS idx_enterprise_departments_tree_path
    ON enterprise_departments (enterprise_id, path text_pattern_ops);
CREATE INDEX IF NOT EXISTS idx_enterprise_departments_parent
    ON enterprise_departments (enterprise_id, parent_id) WHERE deleted_at IS NULL;
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'enterprise_departments_depth_check') THEN
        ALTER TABLE enterprise_departments ADD CONSTRAINT enterprise_departments_depth_check CHECK (depth BETWEEN 1 AND 8);
    END IF;
END $$;

-- Jobs already submitted before the tree migration froze a single root scope.
-- Preserve that scope without consulting live membership during capture.
ALTER TABLE batch_image_jobs ADD COLUMN IF NOT EXISTS enterprise_department_path TEXT NOT NULL DEFAULT '';
UPDATE batch_image_jobs SET enterprise_department_path = '/' || enterprise_department_id::text || '/'
WHERE enterprise_department_id IS NOT NULL AND enterprise_department_path = '';
