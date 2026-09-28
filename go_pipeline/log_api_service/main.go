package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const ensureRulesTableSQL = `
CREATE TABLE IF NOT EXISTS log_match_rules (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	rule_type TEXT NOT NULL CHECK (rule_type IN ('keyword', 'regex')),
	pattern TEXT NOT NULL,
	enabled BOOLEAN NOT NULL DEFAULT TRUE,
	namespace_scope TEXT NOT NULL DEFAULT '',
	pod_scope TEXT NOT NULL DEFAULT '',
	container_scope TEXT NOT NULL DEFAULT '',
	namespace_scopes TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	pod_scopes TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	container_scopes TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`

type Config struct {
	ListenAddr       string
	PostgresHost     string
	PostgresPort     string
	PostgresDatabase string
	PostgresUser     string
	PostgresPassword string
	PostgresSSLMode  string
	SeedRulesJSON    string
}

type App struct {
	db *sql.DB
}

type Rule struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	RuleType        string    `json:"rule_type"`
	Pattern         string    `json:"pattern"`
	Enabled         bool      `json:"enabled"`
	NamespaceScopes []string  `json:"namespace_scopes"`
	PodScopes       []string  `json:"pod_scopes"`
	ContainerScopes []string  `json:"container_scopes"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type RulePayload struct {
	Name            string   `json:"name"`
	RuleType        string   `json:"rule_type"`
	Pattern         string   `json:"pattern"`
	Enabled         *bool    `json:"enabled"`
	NamespaceScope  string   `json:"namespace_scope"`
	PodScope        string   `json:"pod_scope"`
	ContainerScope  string   `json:"container_scope"`
	NamespaceScopes []string `json:"namespace_scopes"`
	PodScopes       []string `json:"pod_scopes"`
	ContainerScopes []string `json:"container_scopes"`
}

func main() {
	cfg := Config{
		ListenAddr:       env("LISTEN_ADDR", ":8080"),
		PostgresHost:     env("POSTGRES_HOST", "log-monitor-postgres"),
		PostgresPort:     env("POSTGRES_PORT", "5432"),
		PostgresDatabase: env("POSTGRES_DATABASE", "logmonitor"),
		PostgresUser:     env("POSTGRES_USER", "logmonitor"),
		PostgresPassword: env("POSTGRES_PASSWORD", "logmonitor"),
		PostgresSSLMode:  env("POSTGRES_SSLMODE", "disable"),
		SeedRulesJSON:    env("SEED_RULES_JSON", ""),
	}

	db, err := sql.Open("pgx", cfg.postgresDSN())
	if err != nil {
		log.Fatalf("postgres open failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("postgres ping failed: %v", err)
	}

	if err := ensureRulesTable(ctx, db); err != nil {
		log.Fatalf("postgres schema init failed: %v", err)
	}

	if err := seedRulesIfEmpty(ctx, db, cfg.SeedRulesJSON); err != nil {
		log.Fatalf("seed rule init failed: %v", err)
	}

	app := &App{db: db}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", app.handleHealth)
	mux.HandleFunc("/rules", app.handleRules)
	mux.HandleFunc("/rules/", app.handleRuleByID)

	log.Printf("starting log-api-service listen=%s postgres=%s/%s", cfg.ListenAddr, cfg.PostgresHost, cfg.PostgresDatabase)
	if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
		log.Fatalf("http server failed: %v", err)
	}
}

func (c Config) postgresDSN() string {
	return fmt.Sprintf(
		"host=%s port=%s dbname=%s user=%s password=%s sslmode=%s",
		c.PostgresHost,
		c.PostgresPort,
		c.PostgresDatabase,
		c.PostgresUser,
		c.PostgresPassword,
		c.PostgresSSLMode,
	)
}

func ensureRulesTable(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, ensureRulesTableSQL); err != nil {
		return err
	}

	migrations := []string{
		`ALTER TABLE log_match_rules ADD COLUMN IF NOT EXISTS namespace_scopes TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[]`,
		`ALTER TABLE log_match_rules ADD COLUMN IF NOT EXISTS pod_scopes TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[]`,
		`ALTER TABLE log_match_rules ADD COLUMN IF NOT EXISTS container_scopes TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[]`,
		`UPDATE log_match_rules
		 SET namespace_scopes = CASE
		   WHEN namespace_scope <> '' AND COALESCE(array_length(namespace_scopes, 1), 0) = 0
		   THEN ARRAY[namespace_scope]
		   ELSE namespace_scopes
		 END,
		     pod_scopes = CASE
		   WHEN pod_scope <> '' AND COALESCE(array_length(pod_scopes, 1), 0) = 0
		   THEN ARRAY[pod_scope]
		   ELSE pod_scopes
		 END,
		     container_scopes = CASE
		   WHEN container_scope <> '' AND COALESCE(array_length(container_scopes, 1), 0) = 0
		   THEN ARRAY[container_scope]
		   ELSE container_scopes
		 END`,
	}

	for _, stmt := range migrations {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	return nil
}

func seedRulesIfEmpty(ctx context.Context, db *sql.DB, seedJSON string) error {
	if strings.TrimSpace(seedJSON) == "" {
		return nil
	}

	var existingCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM log_match_rules`).Scan(&existingCount); err != nil {
		return err
	}
	if existingCount > 0 {
		return nil
	}

	var seeds []RulePayload
	if err := json.Unmarshal([]byte(seedJSON), &seeds); err != nil {
		return fmt.Errorf("invalid SEED_RULES_JSON: %w", err)
	}

	for _, seed := range seeds {
		if err := validateRulePayload(seed); err != nil {
			return fmt.Errorf("invalid seed rule %q: %w", seed.Name, err)
		}

		enabled := true
		if seed.Enabled != nil {
			enabled = *seed.Enabled
		}

		if _, err := db.ExecContext(
			ctx,
			`INSERT INTO log_match_rules (name, rule_type, pattern, enabled, namespace_scope, pod_scope, container_scope, namespace_scopes, pod_scopes, container_scopes, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())`,
			strings.TrimSpace(seed.Name),
			normalizedRuleType(seed.RuleType),
			strings.TrimSpace(seed.Pattern),
			enabled,
			legacyScopeValue(seed.NamespaceScopes, seed.NamespaceScope),
			legacyScopeValue(seed.PodScopes, seed.PodScope),
			legacyScopeValue(seed.ContainerScopes, seed.ContainerScope),
			normalizedScopes(seed.NamespaceScopes, seed.NamespaceScope),
			normalizedScopes(seed.PodScopes, seed.PodScope),
			normalizedScopes(seed.ContainerScopes, seed.ContainerScope),
		); err != nil {
			return err
		}
	}

	log.Printf("seeded %d default rules into postgres", len(seeds))
	return nil
}

func (a *App) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.listRules(w, r)
	case http.MethodPost:
		a.createRule(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *App) handleRuleByID(w http.ResponseWriter, r *http.Request) {
	id, err := parseRuleID(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	switch r.Method {
	case http.MethodGet:
		a.getRule(w, r, id)
	case http.MethodPut:
		a.updateRule(w, r, id)
	case http.MethodDelete:
		a.deleteRule(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *App) listRules(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(
		r.Context(),
		`SELECT id, name, rule_type, pattern, enabled, namespace_scopes, pod_scopes, container_scopes, created_at, updated_at
		 FROM log_match_rules
		 ORDER BY id ASC`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	rules := make([]Rule, 0)
	for rows.Next() {
		var rule Rule
		if err := rows.Scan(
			&rule.ID,
			&rule.Name,
			&rule.RuleType,
			&rule.Pattern,
			&rule.Enabled,
			&rule.NamespaceScopes,
			&rule.PodScopes,
			&rule.ContainerScopes,
			&rule.CreatedAt,
			&rule.UpdatedAt,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rules = append(rules, rule)
	}

	writeJSON(w, http.StatusOK, rules)
}

func (a *App) getRule(w http.ResponseWriter, r *http.Request, id int64) {
	rule, err := fetchRuleByID(r.Context(), a.db, id)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "rule not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, rule)
}

func (a *App) createRule(w http.ResponseWriter, r *http.Request) {
	var payload RulePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if err := validateRulePayload(payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	var created Rule
	namespaceScopes := normalizedScopes(payload.NamespaceScopes, payload.NamespaceScope)
	podScopes := normalizedScopes(payload.PodScopes, payload.PodScope)
	containerScopes := normalizedScopes(payload.ContainerScopes, payload.ContainerScope)
	err := a.db.QueryRowContext(
		r.Context(),
		`INSERT INTO log_match_rules (name, rule_type, pattern, enabled, namespace_scope, pod_scope, container_scope, namespace_scopes, pod_scopes, container_scopes, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
		 RETURNING id, name, rule_type, pattern, enabled, namespace_scopes, pod_scopes, container_scopes, created_at, updated_at`,
		strings.TrimSpace(payload.Name),
		normalizedRuleType(payload.RuleType),
		strings.TrimSpace(payload.Pattern),
		enabled,
		legacyScopeValue(payload.NamespaceScopes, payload.NamespaceScope),
		legacyScopeValue(payload.PodScopes, payload.PodScope),
		legacyScopeValue(payload.ContainerScopes, payload.ContainerScope),
		namespaceScopes,
		podScopes,
		containerScopes,
	).Scan(
		&created.ID,
		&created.Name,
		&created.RuleType,
		&created.Pattern,
		&created.Enabled,
		&created.NamespaceScopes,
		&created.PodScopes,
		&created.ContainerScopes,
		&created.CreatedAt,
		&created.UpdatedAt,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (a *App) updateRule(w http.ResponseWriter, r *http.Request, id int64) {
	var payload RulePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if err := validateRulePayload(payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	var updated Rule
	namespaceScopes := normalizedScopes(payload.NamespaceScopes, payload.NamespaceScope)
	podScopes := normalizedScopes(payload.PodScopes, payload.PodScope)
	containerScopes := normalizedScopes(payload.ContainerScopes, payload.ContainerScope)
	err := a.db.QueryRowContext(
		r.Context(),
		`UPDATE log_match_rules
		 SET name = $2,
		     rule_type = $3,
		     pattern = $4,
		     enabled = $5,
		     namespace_scope = $6,
		     pod_scope = $7,
		     container_scope = $8,
		     namespace_scopes = $9,
		     pod_scopes = $10,
		     container_scopes = $11,
		     updated_at = NOW()
		 WHERE id = $1
		 RETURNING id, name, rule_type, pattern, enabled, namespace_scopes, pod_scopes, container_scopes, created_at, updated_at`,
		id,
		strings.TrimSpace(payload.Name),
		normalizedRuleType(payload.RuleType),
		strings.TrimSpace(payload.Pattern),
		enabled,
		legacyScopeValue(payload.NamespaceScopes, payload.NamespaceScope),
		legacyScopeValue(payload.PodScopes, payload.PodScope),
		legacyScopeValue(payload.ContainerScopes, payload.ContainerScope),
		namespaceScopes,
		podScopes,
		containerScopes,
	).Scan(
		&updated.ID,
		&updated.Name,
		&updated.RuleType,
		&updated.Pattern,
		&updated.Enabled,
		&updated.NamespaceScopes,
		&updated.PodScopes,
		&updated.ContainerScopes,
		&updated.CreatedAt,
		&updated.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "rule not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func (a *App) deleteRule(w http.ResponseWriter, r *http.Request, id int64) {
	result, err := a.db.ExecContext(r.Context(), `DELETE FROM log_match_rules WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rowsAffected == 0 {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func fetchRuleByID(ctx context.Context, db *sql.DB, id int64) (Rule, error) {
	var rule Rule
	err := db.QueryRowContext(
		ctx,
		`SELECT id, name, rule_type, pattern, enabled, namespace_scopes, pod_scopes, container_scopes, created_at, updated_at
		 FROM log_match_rules
		 WHERE id = $1`,
		id,
	).Scan(
		&rule.ID,
		&rule.Name,
		&rule.RuleType,
		&rule.Pattern,
		&rule.Enabled,
		&rule.NamespaceScopes,
		&rule.PodScopes,
		&rule.ContainerScopes,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)
	return rule, err
}

func parseRuleID(path string) (int64, error) {
	idStr := strings.TrimPrefix(path, "/rules/")
	idStr = strings.TrimSpace(idStr)
	if idStr == "" || strings.Contains(idStr, "/") {
		return 0, fmt.Errorf("invalid rule id path")
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid rule id")
	}

	return id, nil
}

func validateRulePayload(payload RulePayload) error {
	if strings.TrimSpace(payload.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(payload.Pattern) == "" {
		return fmt.Errorf("pattern is required")
	}

	switch normalizedRuleType(payload.RuleType) {
	case "keyword":
		return nil
	case "regex":
		if _, err := regexp.Compile(strings.TrimSpace(payload.Pattern)); err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("rule_type must be keyword or regex")
	}
}

func normalizedRuleType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizedScopes(scopes []string, legacyScope string) []string {
	if len(scopes) == 0 {
		if strings.TrimSpace(legacyScope) == "" {
			return []string{}
		}
		scopes = []string{legacyScope}
	}

	out := make([]string, 0, len(scopes))
	seen := map[string]bool{}
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" || seen[scope] {
			continue
		}
		seen[scope] = true
		out = append(out, scope)
	}

	return out
}

func legacyScopeValue(scopes []string, legacyScope string) string {
	normalized := normalizedScopes(scopes, legacyScope)
	if len(normalized) == 1 {
		return normalized[0]
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("json encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
