package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type SyslogEntry struct {
	Timestamp time.Time
	Level     string
	Message   string
}

type RequestLog struct {
	Type               string
	Timestamp          time.Time
	RequestID          string
	VirtualModel       string
	ClientKeyID        int64
	ProviderName       string
	ModelName          string
	StatusCode         int
	Latency            time.Duration
	InputTokens        int
	OutputTokens       int
	CachedTokens       int
	ReasoningTokens    int
	ErrorMessage       string
	RetryCount         int
	FallbackCount      int
	RTKIntercepted     bool
	RTKSavedTokens     int
	CavemanIntercepted bool
	CavemanSavedTokens int
}

type LogRepository struct {
	db *sql.DB
}

func NewLogRepository(db *sql.DB) *LogRepository {
	return &LogRepository{db: db}
}

func (r *LogRepository) InsertRequestLog(ctx context.Context, log RequestLog) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO request_logs (
			log_type, timestamp, request_id, virtual_model, client_key_id,
			provider_name, model_name, status_code, latency_ms,
			input_tokens, output_tokens, cached_tokens, reasoning_tokens,
			error_message, retry_count, fallback_count,
			rtk_intercepted, rtk_saved_tokens, caveman_intercepted, caveman_saved_tokens
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.Type,
		log.Timestamp,
		log.RequestID,
		log.VirtualModel,
		log.ClientKeyID,
		log.ProviderName,
		log.ModelName,
		log.StatusCode,
		log.Latency.Milliseconds(),
		log.InputTokens,
		log.OutputTokens,
		log.CachedTokens,
		log.ReasoningTokens,
		log.ErrorMessage,
		log.RetryCount,
		log.FallbackCount,
		log.RTKIntercepted,
		log.RTKSavedTokens,
		log.CavemanIntercepted,
		log.CavemanSavedTokens,
	)
	if err != nil {
		return fmt.Errorf("insert request log: %w", err)
	}
	return nil
}

func (r *LogRepository) InsertSyslogEntry(ctx context.Context, timestamp time.Time, level, message string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO syslog_entries (timestamp, level, message) VALUES (?, ?, ?)`,
		timestamp, level, message,
	)
	if err != nil {
		return fmt.Errorf("insert syslog entry: %w", err)
	}
	return nil
}

func (r *LogRepository) ListRequestLogs(ctx context.Context, limit int) ([]RequestLog, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT log_type, timestamp, request_id, virtual_model, client_key_id,
			provider_name, model_name, status_code, latency_ms,
			input_tokens, output_tokens, cached_tokens, reasoning_tokens,
			error_message, retry_count, fallback_count,
			rtk_intercepted, rtk_saved_tokens, caveman_intercepted, caveman_saved_tokens
		 FROM request_logs ORDER BY timestamp DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list request logs: %w", err)
	}
	defer rows.Close()

	var logs []RequestLog
	for rows.Next() {
		var l RequestLog
		var latencyMs int64
		if err := rows.Scan(
			&l.Type, &l.Timestamp, &l.RequestID, &l.VirtualModel, &l.ClientKeyID,
			&l.ProviderName, &l.ModelName, &l.StatusCode, &latencyMs,
			&l.InputTokens, &l.OutputTokens, &l.CachedTokens, &l.ReasoningTokens,
			&l.ErrorMessage, &l.RetryCount, &l.FallbackCount,
			&l.RTKIntercepted, &l.RTKSavedTokens, &l.CavemanIntercepted, &l.CavemanSavedTokens,
		); err != nil {
			return nil, fmt.Errorf("scan request log: %w", err)
		}
		l.Latency = time.Duration(latencyMs) * time.Millisecond
		logs = append(logs, l)
	}
	return logs, nil
}

func (r *LogRepository) ListSyslogEntries(ctx context.Context, limit int) ([]SyslogEntry, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT timestamp, level, message FROM syslog_entries ORDER BY timestamp DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list syslog entries: %w", err)
	}
	defer rows.Close()

	var entries []SyslogEntry
	for rows.Next() {
		var e SyslogEntry
		if err := rows.Scan(&e.Timestamp, &e.Level, &e.Message); err != nil {
			return nil, fmt.Errorf("scan syslog entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (r *LogRepository) DeleteOlderThan(ctx context.Context, age time.Duration) error {
	cutoff := time.Now().Add(-age)
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM request_logs WHERE timestamp < ?`, cutoff,
	)
	if err != nil {
		return fmt.Errorf("delete old request logs: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`DELETE FROM syslog_entries WHERE timestamp < ?`, cutoff,
	)
	if err != nil {
		return fmt.Errorf("delete old syslog entries: %w", err)
	}
	return nil
}

type ComputedStats struct {
	TotalRequests     int
	Successes         int
	Failures          int
	InputTokens       int
	OutputTokens      int
	CachedTokens      int
	ReasoningTokens   int
	RTKIntercepts     int
	RTKSavedTokens    int
	CavemanIntercepts int
	CavemanSavedTokens int
	ByVirtualModel    map[string]*ComputedModelStat
	ByProvider        map[string]*ComputedModelStat
}

type ComputedModelStat struct {
	Requests     int
	Successes    int
	Failures     int
	InputTokens  int
	OutputTokens int
	CachedTokens int
}

func (r *LogRepository) ComputeStats(ctx context.Context) (*ComputedStats, error) {
	stats := &ComputedStats{
		ByVirtualModel: make(map[string]*ComputedModelStat),
		ByProvider:     make(map[string]*ComputedModelStat),
	}

	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*), SUM(CASE WHEN status_code >= 200 AND status_code < 300 THEN 1 ELSE 0 END),
			SUM(CASE WHEN status_code < 200 OR status_code >= 300 THEN 1 ELSE 0 END),
			COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(cached_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			SUM(CASE WHEN rtk_intercepted THEN 1 ELSE 0 END), COALESCE(SUM(rtk_saved_tokens), 0),
			SUM(CASE WHEN caveman_intercepted THEN 1 ELSE 0 END), COALESCE(SUM(caveman_saved_tokens), 0)
		 FROM request_logs WHERE log_type = 'proxy'`,
	).Scan(&stats.TotalRequests, &stats.Successes, &stats.Failures, &stats.InputTokens, &stats.OutputTokens, &stats.CachedTokens,
		&stats.ReasoningTokens, &stats.RTKIntercepts, &stats.RTKSavedTokens, &stats.CavemanIntercepts, &stats.CavemanSavedTokens)
	if err != nil {
		return nil, fmt.Errorf("compute global stats: %w", err)
	}

	vmRows, err := r.db.QueryContext(ctx,
		`SELECT virtual_model, COUNT(*),
			SUM(CASE WHEN status_code >= 200 AND status_code < 300 THEN 1 ELSE 0 END),
			SUM(CASE WHEN status_code < 200 OR status_code >= 300 THEN 1 ELSE 0 END),
			COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(cached_tokens), 0)
		 FROM request_logs WHERE log_type = 'proxy'
		 GROUP BY virtual_model ORDER BY COUNT(*) DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("compute vm stats: %w", err)
	}
	defer vmRows.Close()

	for vmRows.Next() {
		var name string
		var ms ComputedModelStat
		if err := vmRows.Scan(&name, &ms.Requests, &ms.Successes, &ms.Failures, &ms.InputTokens, &ms.OutputTokens, &ms.CachedTokens); err != nil {
			return nil, fmt.Errorf("scan vm stats: %w", err)
		}
		stats.ByVirtualModel[name] = &ms
	}

	pmRows, err := r.db.QueryContext(ctx,
		`SELECT provider_name || '/' || model_name, COUNT(*),
			SUM(CASE WHEN status_code >= 200 AND status_code < 300 THEN 1 ELSE 0 END),
			SUM(CASE WHEN status_code < 200 OR status_code >= 300 THEN 1 ELSE 0 END),
			COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(cached_tokens), 0)
		 FROM request_logs WHERE log_type = 'proxy' AND provider_name != ''
		 GROUP BY provider_name, model_name ORDER BY COUNT(*) DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("compute provider stats: %w", err)
	}
	defer pmRows.Close()

	for pmRows.Next() {
		var name string
		var ms ComputedModelStat
		if err := pmRows.Scan(&name, &ms.Requests, &ms.Successes, &ms.Failures, &ms.InputTokens, &ms.OutputTokens, &ms.CachedTokens); err != nil {
			return nil, fmt.Errorf("scan provider stats: %w", err)
		}
		stats.ByProvider[name] = &ms
	}

	return stats, nil
}
