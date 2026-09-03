package storage

const experienceMigrationV42 = `CREATE TABLE experience_records (
    id TEXT PRIMARY KEY,
    experience_key TEXT NOT NULL UNIQUE,
    topic_id TEXT REFERENCES topics(id) ON DELETE CASCADE,
    task_id TEXT REFERENCES tasks(id) ON DELETE CASCADE,
    title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 200),
    summary TEXT NOT NULL CHECK (length(trim(summary)) BETWEEN 1 AND 1000),
    guidance TEXT NOT NULL CHECK (length(trim(guidance)) BETWEEN 1 AND 4000),
    applicability TEXT NOT NULL CHECK (length(trim(applicability)) BETWEEN 1 AND 1000),
    project_wide INTEGER NOT NULL CHECK (project_wide IN (0, 1)),
    status TEXT NOT NULL CHECK (status IN ('CANDIDATE', 'ACTIVE', 'CHALLENGED', 'SUPERSEDED')),
    pinned INTEGER NOT NULL CHECK (pinned IN (0, 1)),
    verification_count INTEGER NOT NULL CHECK (verification_count >= 0),
    successful_applications INTEGER NOT NULL CHECK (successful_applications >= 0),
    failed_applications INTEGER NOT NULL CHECK (failed_applications >= 0),
    source_run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
    supersedes_experience_id TEXT REFERENCES experience_records(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK ((topic_id IS NOT NULL AND task_id IS NULL) OR (topic_id IS NULL AND task_id IS NOT NULL))
);
CREATE INDEX experience_records_topic_idx ON experience_records(topic_id, status, updated_at DESC);
CREATE INDEX experience_records_task_idx ON experience_records(task_id, status, updated_at DESC);
CREATE INDEX experience_records_recall_idx ON experience_records(status, project_wide, pinned, updated_at DESC);

CREATE TABLE experience_recalls (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    experience_id TEXT NOT NULL REFERENCES experience_records(id) ON DELETE CASCADE,
    rank INTEGER NOT NULL CHECK (rank >= 1),
    relevance_score REAL NOT NULL CHECK (relevance_score BETWEEN 0 AND 1),
    utility_score REAL NOT NULL CHECK (utility_score BETWEEN 0 AND 1),
    scope_score REAL NOT NULL CHECK (scope_score BETWEEN 0 AND 1),
    freshness_score REAL NOT NULL CHECK (freshness_score BETWEEN 0 AND 1),
    final_score REAL NOT NULL CHECK (final_score BETWEEN 0 AND 1),
    estimated_tokens INTEGER NOT NULL CHECK (estimated_tokens >= 0),
    outcome TEXT NOT NULL CHECK (outcome IN ('PENDING', 'HELPFUL', 'HARMFUL', 'IGNORED')),
    recalled_at TEXT NOT NULL,
    evaluated_at TEXT,
    UNIQUE(run_id, experience_id)
);
CREATE INDEX experience_recalls_run_idx ON experience_recalls(run_id, rank);
CREATE INDEX experience_recalls_experience_idx ON experience_recalls(experience_id, recalled_at DESC);

DROP TRIGGER IF EXISTS search_documents_ai;
DROP TRIGGER IF EXISTS search_documents_ad;
DROP TRIGGER IF EXISTS search_documents_au;
DROP TABLE search_documents_fts;
CREATE TABLE search_documents_v42 (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN (
        'TOPIC', 'TASK', 'MESSAGE', 'PLAN_REVISION', 'CLARIFICATION',
        'DECISION', 'RUN_SUMMARY', 'CI_CHECK', 'LABEL', 'EXPERIENCE'
    )),
    source_id TEXT NOT NULL,
    subject_kind TEXT NOT NULL CHECK (subject_kind IN ('TOPIC', 'TASK')),
    subject_id TEXT NOT NULL,
    stable_key TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    status TEXT NOT NULL,
    is_current INTEGER NOT NULL CHECK (is_current IN (0, 1)),
    updated_at TEXT NOT NULL,
    UNIQUE(kind, source_id)
);
INSERT INTO search_documents_v42 SELECT * FROM search_documents;
ALTER TABLE search_documents RENAME TO search_documents_v41;
ALTER TABLE search_documents_v42 RENAME TO search_documents;
DROP TABLE search_documents_v41;
CREATE INDEX search_documents_subject_idx ON search_documents(subject_kind, subject_id, updated_at DESC);
CREATE INDEX search_documents_filter_idx ON search_documents(kind, status, is_current, updated_at DESC);
CREATE VIRTUAL TABLE search_documents_fts USING fts5(
    title, body, stable_key,
    content = 'search_documents', content_rowid = 'rowid', tokenize = 'trigram'
);
CREATE TRIGGER search_documents_ai AFTER INSERT ON search_documents BEGIN
    INSERT INTO search_documents_fts(rowid, title, body, stable_key)
    VALUES (new.rowid, new.title, new.body, new.stable_key);
END;
CREATE TRIGGER search_documents_ad AFTER DELETE ON search_documents BEGIN
    INSERT INTO search_documents_fts(search_documents_fts, rowid, title, body, stable_key)
    VALUES ('delete', old.rowid, old.title, old.body, old.stable_key);
END;
CREATE TRIGGER search_documents_au AFTER UPDATE OF title, body, stable_key ON search_documents BEGIN
    INSERT INTO search_documents_fts(search_documents_fts, rowid, title, body, stable_key)
    VALUES ('delete', old.rowid, old.title, old.body, old.stable_key);
    INSERT INTO search_documents_fts(rowid, title, body, stable_key)
    VALUES (new.rowid, new.title, new.body, new.stable_key);
END;
INSERT INTO search_documents_fts(search_documents_fts) VALUES ('rebuild');

CREATE TRIGGER search_experiences_ai AFTER INSERT ON experience_records BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    VALUES ('EXPERIENCE:' || new.id, 'EXPERIENCE', new.id,
            CASE WHEN new.topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END,
            COALESCE(new.topic_id, new.task_id), new.experience_key, new.title,
            new.summary || char(10) || new.guidance || char(10) || new.applicability,
            new.status, CASE WHEN new.status = 'ACTIVE' THEN 1 ELSE 0 END, new.updated_at);
END;
CREATE TRIGGER search_experiences_au AFTER UPDATE OF title, summary, guidance, applicability, status, updated_at ON experience_records BEGIN
    UPDATE search_documents SET title = new.title,
        body = new.summary || char(10) || new.guidance || char(10) || new.applicability,
        status = new.status, is_current = CASE WHEN new.status = 'ACTIVE' THEN 1 ELSE 0 END,
        updated_at = new.updated_at
    WHERE id = 'EXPERIENCE:' || new.id;
END;
CREATE TRIGGER search_experiences_ad AFTER DELETE ON experience_records BEGIN
    DELETE FROM search_documents WHERE id = 'EXPERIENCE:' || old.id;
END;`
