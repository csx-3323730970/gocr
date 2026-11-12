CREATE TABLE IF NOT EXISTS jobs (
  id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  idem_key    CHAR(64) NOT NULL,
  repo        VARCHAR(255) NOT NULL,
  base_sha    CHAR(40) NOT NULL DEFAULT '',
  head_sha    CHAR(40) NOT NULL DEFAULT '',
  diff        MEDIUMTEXT NULL,
  state       VARCHAR(16) NOT NULL DEFAULT 'queued',
  attempts    INT NOT NULL DEFAULT 0,
  lease_until DATETIME NULL,
  last_error  TEXT NULL,
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_jobs_idem (idem_key),
  KEY idx_jobs_state_created (state, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS findings (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  job_id        BIGINT UNSIGNED NOT NULL,
  file          VARCHAR(512) NOT NULL,
  line          INT NOT NULL,
  severity      VARCHAR(16) NOT NULL,
  rule_name     VARCHAR(128) NOT NULL DEFAULT '',
  message       TEXT NOT NULL,
  snippet       TEXT NULL,
  confidence    DOUBLE NOT NULL DEFAULT 0,
  method        VARCHAR(16) NOT NULL DEFAULT '',
  original_line INT NOT NULL DEFAULT 0,
  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_findings_job (job_id),
  CONSTRAINT fk_findings_job FOREIGN KEY (job_id) REFERENCES jobs (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS llm_cache (
  cache_key  CHAR(64) PRIMARY KEY,
  payload    MEDIUMTEXT NOT NULL,
  tokens     INT NOT NULL DEFAULT 0,
  expires_at DATETIME NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_llm_cache_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
