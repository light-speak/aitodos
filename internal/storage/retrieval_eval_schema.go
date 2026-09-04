package storage

const retrievalEvalMigrationV44 = `CREATE TABLE retrieval_eval_cases (
    id TEXT PRIMARY KEY,
    query TEXT NOT NULL CHECK (length(trim(query)) BETWEEN 1 AND 500),
    kinds_json TEXT NOT NULL CHECK (json_valid(kinds_json)),
    only_current INTEGER NOT NULL CHECK (only_current IN (0, 1)),
    note TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 1000),
    active INTEGER NOT NULL CHECK (active IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(query, kinds_json, only_current)
);
CREATE TABLE retrieval_eval_relevances (
    case_id TEXT NOT NULL REFERENCES retrieval_eval_cases(id) ON DELETE RESTRICT,
    document_id TEXT NOT NULL CHECK (length(trim(document_id)) BETWEEN 3 AND 300),
    created_at TEXT NOT NULL,
    PRIMARY KEY(case_id, document_id)
);
CREATE TABLE retrieval_eval_runs (
    id TEXT PRIMARY KEY,
    engine TEXT NOT NULL CHECK (length(trim(engine)) BETWEEN 1 AND 100),
    k INTEGER NOT NULL CHECK (k BETWEEN 1 AND 50),
    case_count INTEGER NOT NULL CHECK (case_count > 0),
    relevant_count INTEGER NOT NULL CHECK (relevant_count > 0),
    recalled_count INTEGER NOT NULL CHECK (recalled_count BETWEEN 0 AND relevant_count),
    hit_cases INTEGER NOT NULL CHECK (hit_cases BETWEEN 0 AND case_count),
    recall_at_k REAL NOT NULL CHECK (recall_at_k BETWEEN 0 AND 1),
    hit_at_k REAL NOT NULL CHECK (hit_at_k BETWEEN 0 AND 1),
    mrr REAL NOT NULL CHECK (mrr BETWEEN 0 AND 1),
    created_at TEXT NOT NULL
);
CREATE TABLE retrieval_eval_results (
    run_id TEXT NOT NULL REFERENCES retrieval_eval_runs(id) ON DELETE CASCADE,
    case_id TEXT NOT NULL REFERENCES retrieval_eval_cases(id) ON DELETE RESTRICT,
    document_id TEXT NOT NULL,
    rank INTEGER CHECK (rank IS NULL OR rank >= 1),
    PRIMARY KEY(run_id, case_id, document_id)
);
CREATE INDEX retrieval_eval_cases_active_idx ON retrieval_eval_cases(active, updated_at DESC, id DESC);
CREATE INDEX retrieval_eval_runs_recent_idx ON retrieval_eval_runs(created_at DESC, id DESC);`
