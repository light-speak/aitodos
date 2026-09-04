package storage

const searchKnowledgeMigrationV40 = `DROP TRIGGER IF EXISTS search_documents_ai;
DROP TRIGGER IF EXISTS search_documents_ad;
DROP TRIGGER IF EXISTS search_documents_au;
DROP TABLE search_documents_fts;
DROP TABLE search_documents;

CREATE TABLE search_documents (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN (
        'TOPIC', 'TASK', 'MESSAGE', 'PLAN_REVISION', 'CLARIFICATION',
        'DECISION', 'RUN_SUMMARY', 'CI_CHECK', 'LABEL'
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

CREATE TRIGGER search_decisions_ai AFTER INSERT ON decisions BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    VALUES ('DECISION:' || new.id, 'DECISION', new.id,
            CASE WHEN new.topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END,
            COALESCE(new.topic_id, new.task_id), new.decision_key, new.title, new.content,
            new.status, CASE WHEN new.status = 'ACTIVE' THEN 1 ELSE 0 END, new.created_at);
END;
CREATE TRIGGER search_decisions_au AFTER UPDATE OF status ON decisions BEGIN
    UPDATE search_documents SET status = new.status,
        is_current = CASE WHEN new.status = 'ACTIVE' THEN 1 ELSE 0 END
    WHERE id = 'DECISION:' || new.id;
END;
CREATE TRIGGER search_decisions_ad AFTER DELETE ON decisions BEGIN
    DELETE FROM search_documents WHERE id = 'DECISION:' || old.id;
END;

CREATE TRIGGER search_run_summaries_ai AFTER INSERT ON run_summaries BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    SELECT 'RUN_SUMMARY:' || new.run_id, 'RUN_SUMMARY', new.run_id,
           CASE WHEN runs.topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END,
           COALESCE(runs.topic_id, runs.task_id), runs.id,
           runs.purpose || ' Run 摘要', new.summary, new.status, 1, new.created_at
    FROM runs WHERE runs.id = new.run_id;
END;
CREATE TRIGGER search_run_summaries_au AFTER UPDATE OF status, summary, created_at ON run_summaries BEGIN
    UPDATE search_documents SET body = new.summary, status = new.status, updated_at = new.created_at
    WHERE id = 'RUN_SUMMARY:' || new.run_id;
END;
CREATE TRIGGER search_run_summaries_ad AFTER DELETE ON run_summaries BEGIN
    DELETE FROM search_documents WHERE id = 'RUN_SUMMARY:' || old.run_id;
END;

CREATE TRIGGER search_ci_snapshots_ai AFTER INSERT ON ci_check_snapshots BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    VALUES ('CI_CHECK:' || new.id, 'CI_CHECK', new.id, 'TASK', new.task_id,
            new.commit_sha, new.provider || ' CI', new.checks_json, new.state, 1, new.observed_at);
END;
CREATE TRIGGER search_ci_snapshots_ad AFTER DELETE ON ci_check_snapshots BEGIN
    DELETE FROM search_documents WHERE id = 'CI_CHECK:' || old.id;
END;

CREATE TRIGGER search_topic_labels_ai AFTER INSERT ON topic_labels BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    SELECT 'LABEL:TOPIC:' || new.topic_id || ':' || labels.id, 'LABEL', new.topic_id || ':' || labels.id,
           'TOPIC', new.topic_id, labels.name, labels.name, labels.color, 'ATTACHED', 1, new.created_at
    FROM labels WHERE labels.id = new.label_id;
END;
CREATE TRIGGER search_topic_labels_ad AFTER DELETE ON topic_labels BEGIN
    DELETE FROM search_documents WHERE id = 'LABEL:TOPIC:' || old.topic_id || ':' || old.label_id;
END;
CREATE TRIGGER search_task_labels_ai AFTER INSERT ON task_labels BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    SELECT 'LABEL:TASK:' || new.task_id || ':' || labels.id, 'LABEL', new.task_id || ':' || labels.id,
           'TASK', new.task_id, labels.name, labels.name, labels.color, 'ATTACHED', 1, new.created_at
    FROM labels WHERE labels.id = new.label_id;
END;
CREATE TRIGGER search_task_labels_ad AFTER DELETE ON task_labels BEGIN
    DELETE FROM search_documents WHERE id = 'LABEL:TASK:' || old.task_id || ':' || old.label_id;
END;

INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'TOPIC:' || id, 'TOPIC', id, 'TOPIC', id, topic_key, title, description, status, 1, updated_at FROM topics;
INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'TASK:' || id, 'TASK', id, 'TASK', id, task_key, title, description || char(10) || acceptance_criteria, status, 1, updated_at FROM tasks;
INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'MESSAGE:' || messages.id, 'MESSAGE', messages.id,
       CASE WHEN threads.topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END, COALESCE(threads.topic_id, threads.task_id),
       COALESCE(topics.topic_key, tasks.task_key) || '#message-' || messages.sequence,
       CASE messages.author_kind WHEN 'HUMAN' THEN '你的消息' WHEN 'AGENT' THEN 'Agent 消息' ELSE '系统消息' END,
       messages.content, messages.author_kind, 1, messages.created_at
FROM messages JOIN threads ON threads.id = messages.thread_id
LEFT JOIN topics ON topics.id = threads.topic_id LEFT JOIN tasks ON tasks.id = threads.task_id;
INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'PLAN_REVISION:' || revisions.id, 'PLAN_REVISION', revisions.id, 'TOPIC', plans.topic_id,
       plans.plan_key || '-R' || revisions.revision_number, plans.plan_key || ' Revision ' || revisions.revision_number,
       revisions.summary || char(10) || revisions.rationale || char(10) || revisions.risks || char(10) ||
       COALESCE((SELECT group_concat(drafts.title || char(10) || drafts.description || char(10) || drafts.acceptance_criteria, char(10))
                 FROM plan_task_drafts drafts WHERE drafts.plan_revision_id = revisions.id), ''),
       plans.status, CASE WHEN plans.current_revision_id = revisions.id THEN 1 ELSE 0 END, plans.updated_at
FROM plan_revisions revisions JOIN plans ON plans.id = revisions.plan_id;
INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'CLARIFICATION:' || clarifications.id, 'CLARIFICATION', clarifications.id,
       CASE WHEN clarifications.topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END,
       COALESCE(clarifications.topic_id, clarifications.task_id),
       COALESCE(topics.topic_key, tasks.task_key) || '#clarification-' || clarifications.id,
       COALESCE(topics.topic_key, tasks.task_key) || ' · ' || clarifications.category,
       clarifications.question || char(10) || clarifications.options_json || char(10) ||
       clarifications.selected_option_id || char(10) || clarifications.custom_answer,
       clarifications.status, 1, clarifications.updated_at
FROM clarifications LEFT JOIN topics ON topics.id = clarifications.topic_id
LEFT JOIN tasks ON tasks.id = clarifications.task_id;
INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'DECISION:' || id, 'DECISION', id, CASE WHEN topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END,
       COALESCE(topic_id, task_id), decision_key, title, content, status,
       CASE WHEN status = 'ACTIVE' THEN 1 ELSE 0 END, created_at FROM decisions;
INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'RUN_SUMMARY:' || summaries.run_id, 'RUN_SUMMARY', summaries.run_id,
       CASE WHEN runs.topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END, COALESCE(runs.topic_id, runs.task_id),
       runs.id, runs.purpose || ' Run 摘要', summaries.summary, summaries.status, 1, summaries.created_at
FROM run_summaries summaries JOIN runs ON runs.id = summaries.run_id;
INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'CI_CHECK:' || id, 'CI_CHECK', id, 'TASK', task_id, commit_sha, provider || ' CI', checks_json,
       state, 1, observed_at FROM ci_check_snapshots;
INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'LABEL:TOPIC:' || bindings.topic_id || ':' || labels.id, 'LABEL', bindings.topic_id || ':' || labels.id,
       'TOPIC', bindings.topic_id, labels.name, labels.name, labels.color, 'ATTACHED', 1, bindings.created_at
FROM topic_labels bindings JOIN labels ON labels.id = bindings.label_id;
INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'LABEL:TASK:' || bindings.task_id || ':' || labels.id, 'LABEL', bindings.task_id || ':' || labels.id,
       'TASK', bindings.task_id, labels.name, labels.name, labels.color, 'ATTACHED', 1, bindings.created_at
FROM task_labels bindings JOIN labels ON labels.id = bindings.label_id;`
