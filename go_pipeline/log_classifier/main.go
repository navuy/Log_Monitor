package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
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
	RedisAddr       string
	RedisStreamGlob string
	DiscoverEvery   time.Duration
	ReadBlock       time.Duration
	StartFromEnd    bool

	InfluxURL    string
	InfluxToken  string
	InfluxOrg    string
	InfluxBucket string

	IgnoredNamespaces []string
	RuleRefreshEvery  time.Duration

	PostgresHost     string
	PostgresPort     string
	PostgresDatabase string
	PostgresUser     string
	PostgresPassword string
	PostgresSSLMode  string
}

type Rule struct {
	ID              int64
	Name            string
	RuleType        string
	Pattern         string
	NamespaceScopes []string
	PodScopes       []string
	ContainerScopes []string
	compiledRegex   *regexp.Regexp
}

type MatchResult struct {
	Type      string
	Value     string
	RuleID    int64
	RuleName  string
	RuleScope string
}

func main() {
	cfg := Config{
		RedisAddr:         env("REDIS_ADDR", "log-monitor-redis:6379"),
		RedisStreamGlob:   env("REDIS_STREAM_GLOB", "logs:k8s*"),
		DiscoverEvery:     durationEnv("DISCOVER_EVERY", 10*time.Second),
		ReadBlock:         durationEnv("READ_BLOCK", 5*time.Second),
		StartFromEnd:      boolEnv("START_FROM_END", true),
		InfluxURL:         env("INFLUX_URL", "http://influxdb:8086"),
		InfluxToken:       env("INFLUX_TOKEN", ""),
		InfluxOrg:         env("INFLUX_ORG", "log-monitor"),
		InfluxBucket:      env("INFLUX_BUCKET", "k8s-logs"),
		IgnoredNamespaces: splitCSV(env("IGNORED_NAMESPACES", "log-monitor")),
		RuleRefreshEvery:  durationEnv("RULE_REFRESH_EVERY", 15*time.Second),
		PostgresHost:      env("POSTGRES_HOST", "log-monitor-postgres"),
		PostgresPort:      env("POSTGRES_PORT", "5432"),
		PostgresDatabase:  env("POSTGRES_DATABASE", "logmonitor"),
		PostgresUser:      env("POSTGRES_USER", "logmonitor"),
		PostgresPassword:  env("POSTGRES_PASSWORD", "logmonitor"),
		PostgresSSLMode:   env("POSTGRES_SSLMODE", "disable"),
	}

	ctx := context.Background()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("redis connection failed: %v", err)
	}

	influx := influxdb2.NewClient(cfg.InfluxURL, cfg.InfluxToken)
	defer influx.Close()
	writeAPI := influx.WriteAPIBlocking(cfg.InfluxOrg, cfg.InfluxBucket)

	db, err := sql.Open("pgx", cfg.postgresDSN())
	if err != nil {
		log.Fatalf("postgres open failed: %v", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("postgres ping failed: %v", err)
	}

	if err := ensureRulesTable(ctx, db); err != nil {
		log.Fatalf("postgres schema init failed: %v", err)
	}

	log.Printf(
		"starting log-classifier redis=%s influx=%s bucket=%s postgres=%s/%s ignored_namespaces=%s refresh=%s",
		cfg.RedisAddr,
		cfg.InfluxURL,
		cfg.InfluxBucket,
		cfg.PostgresHost,
		cfg.PostgresDatabase,
		strings.Join(cfg.IgnoredNamespaces, ","),
		cfg.RuleRefreshEvery,
	)

	rules, err := loadRules(ctx, db)
	if err != nil {
		log.Printf("initial rule load failed: %v", err)
		rules = nil
	}
	log.Printf("loaded %d runtime rules from postgres", len(rules))

	streamIDs := map[string]string{}
	lastDiscover := time.Time{}
	lastRuleRefresh := time.Now()

	for {
		if time.Since(lastDiscover) >= cfg.DiscoverEvery {
			discoverStreams(ctx, rdb, cfg, streamIDs)
			lastDiscover = time.Now()
		}

		if time.Since(lastRuleRefresh) >= cfg.RuleRefreshEvery {
			latestRules, loadErr := loadRules(ctx, db)
			if loadErr != nil {
				log.Printf("rule refresh failed; keeping previous rules: %v", loadErr)
			} else {
				rules = latestRules
				log.Printf("refreshed runtime rules from postgres: %d active", len(rules))
			}
			lastRuleRefresh = time.Now()
		}

		if len(streamIDs) == 0 {
			time.Sleep(1 * time.Second)
			continue
		}

		streamArgs := make([]string, 0, len(streamIDs)*2)
		for stream := range streamIDs {
			streamArgs = append(streamArgs, stream)
		}
		for _, stream := range streamArgs[:len(streamIDs)] {
			streamArgs = append(streamArgs, streamIDs[stream])
		}

		results, readErr := rdb.XRead(ctx, &redis.XReadArgs{
			Streams: streamArgs,
			Count:   100,
			Block:   cfg.ReadBlock,
		}).Result()
		if readErr != nil {
			if readErr == redis.Nil {
				continue
			}

			log.Printf("xread error: %v", readErr)
			time.Sleep(1 * time.Second)
			continue
		}

		for _, stream := range results {
			for _, msg := range stream.Messages {
				streamIDs[stream.Stream] = msg.ID

				entry := normalize(msg.Values)
				if isIgnoredNamespace(entry["namespace"], cfg.IgnoredNamespaces) {
					continue
				}

				match := detectMatch(entry, rules)
				if match == nil {
					continue
				}

				point := influxdb2.NewPoint(
					"k8s_log_alerts",
					map[string]string{
						"namespace":      entry["namespace"],
						"pod_name":       entry["pod_name"],
						"container_name": entry["container_name"],
						"stream":         entry["stream"],
						"tag":            entry["tag"],
						"level":          match.Value,
						"match_type":     match.Type,
						"rule_name":      match.RuleName,
					},
					map[string]interface{}{
						"log":          entry["log"],
						"raw":          entry["raw"],
						"redis_stream": stream.Stream,
						"redis_id":     msg.ID,
						"container_id": entry["container_id"],
						"timestamp":    entry["timestamp"],
						"ingested_at":  entry["ingested_at"],
						"rule_id":      match.RuleID,
						"rule_scope":   match.RuleScope,
					},
					parseLogTimestamp(entry["timestamp"]),
				)

				if err := writeAPI.WritePoint(ctx, point); err != nil {
					log.Printf("failed to write influx stream=%s id=%s: %v", stream.Stream, msg.ID, err)
					continue
				}

				log.Printf(
					"stored alert match_type=%s rule=%s pattern=%s namespace=%s pod=%s container=%s id=%s",
					match.Type,
					match.RuleName,
					match.Value,
					entry["namespace"],
					entry["pod_name"],
					entry["container_name"],
					msg.ID,
				)
			}
		}
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

func loadRules(ctx context.Context, db *sql.DB) ([]Rule, error) {
	rows, err := db.QueryContext(
		ctx,
		`SELECT id, name, rule_type, pattern, namespace_scopes, pod_scopes, container_scopes
		 FROM log_match_rules
		 WHERE enabled = TRUE
		 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, err
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
			&rule.NamespaceScopes,
			&rule.PodScopes,
			&rule.ContainerScopes,
		); err != nil {
			return nil, err
		}

		rule.RuleType = strings.ToLower(strings.TrimSpace(rule.RuleType))
		if rule.RuleType != "keyword" && rule.RuleType != "regex" {
			log.Printf("skipping unsupported rule_type=%q for rule id=%d", rule.RuleType, rule.ID)
			continue
		}

		if rule.RuleType == "regex" {
			compiled, compileErr := regexp.Compile(rule.Pattern)
			if compileErr != nil {
				log.Printf("skipping invalid regex rule id=%d name=%s pattern=%q: %v", rule.ID, rule.Name, rule.Pattern, compileErr)
				continue
			}
			rule.compiledRegex = compiled
		}

		rules = append(rules, rule)
	}

	return rules, rows.Err()
}

func discoverStreams(ctx context.Context, rdb *redis.Client, cfg Config, streamIDs map[string]string) {
	var cursor uint64

	for {
		keys, next, err := rdb.Scan(ctx, cursor, cfg.RedisStreamGlob, 100).Result()
		if err != nil {
			log.Printf("redis scan error: %v", err)
			return
		}

		for _, key := range keys {
			if _, exists := streamIDs[key]; exists {
				continue
			}

			if cfg.StartFromEnd {
				streamIDs[key] = "$"
			} else {
				streamIDs[key] = "0-0"
			}

			log.Printf("discovered redis stream=%s start_id=%s", key, streamIDs[key])
		}

		cursor = next
		if cursor == 0 {
			break
		}
	}
}

func normalize(values map[string]interface{}) map[string]string {
	out := map[string]string{}

	for key, value := range values {
		if value == nil {
			out[key] = ""
			continue
		}

		out[key] = strings.TrimSpace(toString(value))
	}

	return out
}

func detectMatch(entry map[string]string, rules []Rule) *MatchResult {
	if len(rules) == 0 {
		return nil
	}

	textUpper := strings.ToUpper(entry["log"] + "\n" + entry["raw"])
	rawText := entry["log"] + "\n" + entry["raw"]

	for _, rule := range rules {
		if !ruleMatchesScope(rule, entry) {
			continue
		}

		switch rule.RuleType {
		case "keyword":
			if strings.Contains(textUpper, strings.ToUpper(rule.Pattern)) {
				return &MatchResult{
					Type:      "keyword",
					Value:     rule.Pattern,
					RuleID:    rule.ID,
					RuleName:  rule.Name,
					RuleScope: scopeSummary(rule),
				}
			}
		case "regex":
			if rule.compiledRegex != nil && rule.compiledRegex.MatchString(rawText) {
				return &MatchResult{
					Type:      "regex",
					Value:     rule.Pattern,
					RuleID:    rule.ID,
					RuleName:  rule.Name,
					RuleScope: scopeSummary(rule),
				}
			}
		}
	}

	return nil
}

func ruleMatchesScope(rule Rule, entry map[string]string) bool {
	if !scopeMatches(rule.NamespaceScopes, entry["namespace"]) {
		return false
	}
	if !scopeMatches(rule.PodScopes, entry["pod_name"]) {
		return false
	}
	if !scopeMatches(rule.ContainerScopes, entry["container_name"]) {
		return false
	}

	return true
}

func scopeSummary(rule Rule) string {
	parts := make([]string, 0, 3)
	if len(rule.NamespaceScopes) > 0 {
		parts = append(parts, "namespaces="+strings.Join(rule.NamespaceScopes, "|"))
	}
	if len(rule.PodScopes) > 0 {
		parts = append(parts, "pods="+strings.Join(rule.PodScopes, "|"))
	}
	if len(rule.ContainerScopes) > 0 {
		parts = append(parts, "containers="+strings.Join(rule.ContainerScopes, "|"))
	}
	if len(parts) == 0 {
		return "global"
	}

	return strings.Join(parts, ",")
}

func scopeMatches(allowed []string, actual string) bool {
	if len(allowed) == 0 {
		return true
	}

	actual = strings.TrimSpace(actual)
	for _, candidate := range allowed {
		if strings.TrimSpace(candidate) == actual {
			return true
		}
	}

	return false
}

func isIgnoredNamespace(namespace string, ignoredNamespaces []string) bool {
	namespace = strings.TrimSpace(namespace)

	for _, ignored := range ignoredNamespaces {
		if namespace == strings.TrimSpace(ignored) {
			return true
		}
	}

	return false
}

func parseLogTimestamp(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Now().UTC()
	}

	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Now().UTC()
	}

	return parsed
}

func toString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}

	return out
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		log.Printf("invalid duration %s=%q; using %s", key, raw, fallback)
		return fallback
	}

	return value
}

func boolEnv(key string, fallback bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}

	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "y":
		return true
	case "false", "0", "no", "n":
		return false
	default:
		log.Printf("invalid bool %s=%q; using %t", key, raw, fallback)
		return fallback
	}
}
