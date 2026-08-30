package storage

const searchMigrationV32 = `CREATE TABLE search_documents (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('TOPIC', 'TASK', 'MESSAGE', 'PLAN_REVISION', 'CLARIFICATION')),
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
CREATE TRIGGER search_documents_au AFTER UPDATE ON search_documents BEGIN
    INSERT INTO search_documents_fts(search_documents_fts, rowid, title, body, stable_key)
    VALUES ('delete', old.rowid, old.title, old.body, old.stable_key);
    INSERT INTO search_documents_fts(rowid, title, body, stable_key)
    VALUES (new.rowid, new.title, new.body, new.stable_key);
END;

CREATE TRIGGER search_topics_ai AFTER INSERT ON topics BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    VALUES ('TOPIC:' || new.id, 'TOPIC', new.id, 'TOPIC', new.id, new.topic_key, new.title, new.description, new.status, 1, new.updated_at);
END;
CREATE TRIGGER search_topics_au AFTER UPDATE OF title, description, status, updated_at ON topics BEGIN
    UPDATE search_documents SET title = new.title, body = new.description, status = new.status, updated_at = new.updated_at
    WHERE id = 'TOPIC:' || new.id;
END;
CREATE TRIGGER search_topics_ad AFTER DELETE ON topics BEGIN
    DELETE FROM search_documents WHERE id = 'TOPIC:' || old.id;
END;

CREATE TRIGGER search_tasks_ai AFTER INSERT ON tasks BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    VALUES ('TASK:' || new.id, 'TASK', new.id, 'TASK', new.id, new.task_key, new.title,
            new.description || char(10) || new.acceptance_criteria, new.status, 1, new.updated_at);
END;
CREATE TRIGGER search_tasks_au AFTER UPDATE OF title, description, acceptance_criteria, status, updated_at ON tasks BEGIN
    UPDATE search_documents SET title = new.title,
        body = new.description || char(10) || new.acceptance_criteria,
        status = new.status, updated_at = new.updated_at
    WHERE id = 'TASK:' || new.id;
END;
CREATE TRIGGER search_tasks_ad AFTER DELETE ON tasks BEGIN
    DELETE FROM search_documents WHERE id = 'TASK:' || old.id;
END;

CREATE TRIGGER search_messages_ai AFTER INSERT ON messages BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    SELECT 'MESSAGE:' || new.id, 'MESSAGE', new.id,
           CASE WHEN threads.topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END,
           COALESCE(threads.topic_id, threads.task_id),
           COALESCE(topics.topic_key, tasks.task_key) || '#message-' || new.sequence,
           CASE new.author_kind WHEN 'HUMAN' THEN '你的消息' WHEN 'AGENT' THEN 'Agent 消息' ELSE '系统消息' END,
           new.content, new.author_kind, 1, new.created_at
    FROM threads LEFT JOIN topics ON topics.id = threads.topic_id
    LEFT JOIN tasks ON tasks.id = threads.task_id WHERE threads.id = new.thread_id;
END;
CREATE TRIGGER search_messages_ad AFTER DELETE ON messages BEGIN
    DELETE FROM search_documents WHERE id = 'MESSAGE:' || old.id;
END;

CREATE TRIGGER search_plan_revisions_ai AFTER INSERT ON plan_revisions BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    SELECT 'PLAN_REVISION:' || new.id, 'PLAN_REVISION', new.id, 'TOPIC', plans.topic_id,
           plans.plan_key || '-R' || new.revision_number,
           plans.plan_key || ' Revision ' || new.revision_number,
           new.summary || char(10) || new.rationale || char(10) || new.risks,
           plans.status, CASE WHEN plans.current_revision_id = new.id THEN 1 ELSE 0 END, plans.updated_at
    FROM plans WHERE plans.id = new.plan_id;
END;
CREATE TRIGGER search_plan_revisions_ad AFTER DELETE ON plan_revisions BEGIN
    DELETE FROM search_documents WHERE id = 'PLAN_REVISION:' || old.id;
END;
CREATE TRIGGER search_plans_au AFTER UPDATE OF status, current_revision_id, updated_at ON plans BEGIN
    UPDATE search_documents SET status = new.status,
        is_current = CASE WHEN source_id = new.current_revision_id THEN 1 ELSE 0 END,
        updated_at = new.updated_at
    WHERE kind = 'PLAN_REVISION' AND source_id IN (SELECT id FROM plan_revisions WHERE plan_id = new.id);
END;

CREATE TRIGGER search_plan_drafts_ai AFTER INSERT ON plan_task_drafts BEGIN
    UPDATE search_documents SET body = body || char(10) || new.title || char(10) || new.description || char(10) || new.acceptance_criteria
    WHERE id = 'PLAN_REVISION:' || new.plan_revision_id;
END;
CREATE TRIGGER search_plan_drafts_au AFTER UPDATE OF title, description, acceptance_criteria ON plan_task_drafts BEGIN
    UPDATE search_documents SET body = (
        SELECT revisions.summary || char(10) || revisions.rationale || char(10) || revisions.risks || char(10) ||
               COALESCE(group_concat(drafts.title || char(10) || drafts.description || char(10) || drafts.acceptance_criteria, char(10)), '')
        FROM plan_revisions AS revisions LEFT JOIN plan_task_drafts AS drafts ON drafts.plan_revision_id = revisions.id
        WHERE revisions.id = new.plan_revision_id GROUP BY revisions.id
    ) WHERE id = 'PLAN_REVISION:' || new.plan_revision_id;
END;
CREATE TRIGGER search_plan_drafts_ad AFTER DELETE ON plan_task_drafts BEGIN
    UPDATE search_documents SET body = (
        SELECT revisions.summary || char(10) || revisions.rationale || char(10) || revisions.risks || char(10) ||
               COALESCE(group_concat(drafts.title || char(10) || drafts.description || char(10) || drafts.acceptance_criteria, char(10)), '')
        FROM plan_revisions AS revisions LEFT JOIN plan_task_drafts AS drafts ON drafts.plan_revision_id = revisions.id
        WHERE revisions.id = old.plan_revision_id GROUP BY revisions.id
    ) WHERE id = 'PLAN_REVISION:' || old.plan_revision_id;
END;

CREATE TRIGGER search_clarifications_ai AFTER INSERT ON clarifications BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    SELECT 'CLARIFICATION:' || new.id, 'CLARIFICATION', new.id, 'TASK', new.task_id,
           tasks.task_key || '#clarification-' || new.id, tasks.task_key || ' · ' || new.category,
           new.question || char(10) || new.options_json || char(10) || new.selected_option_id || char(10) || new.custom_answer,
           new.status, 1, new.updated_at FROM tasks WHERE tasks.id = new.task_id;
END;
CREATE TRIGGER search_clarifications_au AFTER UPDATE OF question, options_json, selected_option_id, custom_answer, status, updated_at ON clarifications BEGIN
    UPDATE search_documents SET
        body = new.question || char(10) || new.options_json || char(10) || new.selected_option_id || char(10) || new.custom_answer,
        status = new.status, updated_at = new.updated_at
    WHERE id = 'CLARIFICATION:' || new.id;
END;
CREATE TRIGGER search_clarifications_ad AFTER DELETE ON clarifications BEGIN
    DELETE FROM search_documents WHERE id = 'CLARIFICATION:' || old.id;
END;

INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'TOPIC:' || id, 'TOPIC', id, 'TOPIC', id, topic_key, title, description, status, 1, updated_at FROM topics;
INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'TASK:' || id, 'TASK', id, 'TASK', id, task_key, title,
       description || char(10) || acceptance_criteria, status, 1, updated_at FROM tasks;
INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'MESSAGE:' || messages.id, 'MESSAGE', messages.id,
       CASE WHEN threads.topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END,
       COALESCE(threads.topic_id, threads.task_id),
       COALESCE(topics.topic_key, tasks.task_key) || '#message-' || messages.sequence,
       CASE messages.author_kind WHEN 'HUMAN' THEN '你的消息' WHEN 'AGENT' THEN 'Agent 消息' ELSE '系统消息' END,
       messages.content, messages.author_kind, 1, messages.created_at
FROM messages JOIN threads ON threads.id = messages.thread_id
LEFT JOIN topics ON topics.id = threads.topic_id LEFT JOIN tasks ON tasks.id = threads.task_id;
INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'PLAN_REVISION:' || revisions.id, 'PLAN_REVISION', revisions.id, 'TOPIC', plans.topic_id,
       plans.plan_key || '-R' || revisions.revision_number,
       plans.plan_key || ' Revision ' || revisions.revision_number,
       revisions.summary || char(10) || revisions.rationale || char(10) || revisions.risks || char(10) ||
       COALESCE((SELECT group_concat(drafts.title || char(10) || drafts.description || char(10) || drafts.acceptance_criteria, char(10))
                 FROM plan_task_drafts AS drafts WHERE drafts.plan_revision_id = revisions.id), ''),
       plans.status, CASE WHEN plans.current_revision_id = revisions.id THEN 1 ELSE 0 END, plans.updated_at
FROM plan_revisions AS revisions JOIN plans ON plans.id = revisions.plan_id;
INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'CLARIFICATION:' || clarifications.id, 'CLARIFICATION', clarifications.id, 'TASK', clarifications.task_id,
       tasks.task_key || '#clarification-' || clarifications.id,
       tasks.task_key || ' · ' || clarifications.category,
       clarifications.question || char(10) || clarifications.options_json || char(10) ||
       clarifications.selected_option_id || char(10) || clarifications.custom_answer,
       clarifications.status, 1, clarifications.updated_at
FROM clarifications JOIN tasks ON tasks.id = clarifications.task_id;`

const searchOptimizationMigrationV33 = `DROP TRIGGER IF EXISTS search_documents_au;
CREATE TRIGGER search_documents_au AFTER UPDATE OF title, body, stable_key ON search_documents BEGIN
    INSERT INTO search_documents_fts(search_documents_fts, rowid, title, body, stable_key)
    VALUES ('delete', old.rowid, old.title, old.body, old.stable_key);
    INSERT INTO search_documents_fts(rowid, title, body, stable_key)
    VALUES (new.rowid, new.title, new.body, new.stable_key);
END;

DROP TRIGGER IF EXISTS search_topics_au;
DROP TRIGGER IF EXISTS search_topics_au_content;
DROP TRIGGER IF EXISTS search_topics_au_metadata;
CREATE TRIGGER search_topics_au_content AFTER UPDATE OF title, description ON topics BEGIN
    UPDATE search_documents SET title = new.title, body = new.description
    WHERE id = 'TOPIC:' || new.id;
END;
CREATE TRIGGER search_topics_au_metadata AFTER UPDATE OF status, updated_at ON topics BEGIN
    UPDATE search_documents SET status = new.status, updated_at = new.updated_at
    WHERE id = 'TOPIC:' || new.id;
END;

DROP TRIGGER IF EXISTS search_tasks_au;
DROP TRIGGER IF EXISTS search_tasks_au_content;
DROP TRIGGER IF EXISTS search_tasks_au_metadata;
CREATE TRIGGER search_tasks_au_content AFTER UPDATE OF title, description, acceptance_criteria ON tasks BEGIN
    UPDATE search_documents SET title = new.title,
        body = new.description || char(10) || new.acceptance_criteria
    WHERE id = 'TASK:' || new.id;
END;
CREATE TRIGGER search_tasks_au_metadata AFTER UPDATE OF status, updated_at ON tasks BEGIN
    UPDATE search_documents SET status = new.status, updated_at = new.updated_at
    WHERE id = 'TASK:' || new.id;
END;`
