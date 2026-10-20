package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
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
	ConsumerGroup   string
	ConsumerName    string
	DiscoverEvery   time.Duration
	ReadBlock       time.Duration
	StartFromEnd    bool
	ClaimEvery      time.Duration
	ClaimMinIdle    time.Duration

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
		ConsumerGroup:     env("CONSUMER_GROUP", "log-classifiers"),
		ConsumerName:      env("CONSUMER_NAME", hostname()),
		DiscoverEvery:     durationEnv("DISCOVER_EVERY", 10*time.Second),
		ReadBlock:         durationEnv("READ_BLOCK", 5*time.Second),
		StartFromEnd:      boolEnv("START_FROM_END", true),
		ClaimEvery:        durationEnv("CLAIM_EVERY", 30*time.Second),
		ClaimMinIdle:      durationEnv("CLAIM_MIN_IDLE", time.Minute),
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
		"starting log-classifier redis=%s group=%s consumer=%s influx=%s bucket=%s postgres=%s/%s ignored_namespaces=%s refresh=%s",
		cfg.RedisAddr,
		cfg.ConsumerGroup,
		cfg.ConsumerName,
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

	streams := map[string]struct{}{}
	lastDiscover := time.Time{}
	lastRuleRefresh := time.Now()
	lastClaim := time.Now()

	for {
		if time.Since(lastDiscover) >= cfg.DiscoverEvery {
			discoverStreams(ctx, rdb, cfg, streams)
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

		if len(streams) == 0 {
			time.Sleep(1 * time.Second)
			continue
		}

		if time.Since(lastClaim) >= cfg.ClaimEvery {
			claimPendingMessages(ctx, rdb, writeAPI, cfg, streams, rules)
			lastClaim = time.Now()
		}

		streamArgs := make([]string, 0, len(streams)*2)
		for stream := range streams {
			streamArgs = append(streamArgs, stream)
		}
		for range streams {
			streamArgs = append(streamArgs, ">")
		}

		results, readErr := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    cfg.ConsumerGroup,
			Consumer: cfg.ConsumerName,
			Streams: streamArgs,
			Count:   100,
			Block:   cfg.ReadBlock,
		}).Result()
		if readErr != nil {
			if readErr == redis.Nil {
				continue
			}

			log.Printf("xreadgroup error: %v", readErr)
			time.Sleep(1 * time.Second)
			continue
		}

		for _, stream := range results {
			for _, msg := range stream.Messages {
				processMessage(ctx, rdb, writeAPI, cfg, rules, stream.Stream, msg)
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
		`SELECT id, name, rule_type, pattern,
		        array_to_json(namespace_scopes)::text,
		        array_to_json(pod_scopes)::text,
		        array_to_json(container_scopes)::text
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
		var namespaceScopesJSON, podScopesJSON, containerScopesJSON string
		if err := rows.Scan(
			&rule.ID,
			&rule.Name,
			&rule.RuleType,
			&rule.Pattern,
			&namespaceScopesJSON,
			&podScopesJSON,
			&containerScopesJSON,
		); err != nil {
			return nil, err
		}
		if err := decodeScopes(namespaceScopesJSON, &rule.NamespaceScopes); err != nil {
			return nil, fmt.Errorf("decode namespace scopes for rule %d: %w", rule.ID, err)
		}
		if err := decodeScopes(podScopesJSON, &rule.PodScopes); err != nil {
			return nil, fmt.Errorf("decode pod scopes for rule %d: %w", rule.ID, err)
		}
		if err := decodeScopes(containerScopesJSON, &rule.ContainerScopes); err != nil {
			return nil, fmt.Errorf("decode container scopes for rule %d: %w", rule.ID, err)
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

func decodeScopes(value string, destination *[]string) error {
	if strings.TrimSpace(value) == "" || value == "null" {
		*destination = []string{}
		return nil
	}

	return json.Unmarshal([]byte(value), destination)
}

func discoverStreams(ctx context.Context, rdb *redis.Client, cfg Config, streams map[string]struct{}) {
	var cursor uint64
	discovered := map[string]struct{}{}
	startID := "0-0"
	if cfg.StartFromEnd {
		startID = "$"
	}

	for {
		keys, next, err := rdb.Scan(ctx, cursor, cfg.RedisStreamGlob, 100).Result()
		if err != nil {
			log.Printf("redis scan error: %v", err)
			return
		}

		for _, key := range keys {
			discovered[key] = struct{}{}
			if _, exists := streams[key]; !exists {
				err := rdb.XGroupCreateMkStream(ctx, key, cfg.ConsumerGroup, startID).Err()
				if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
					log.Printf("failed to create consumer group stream=%s group=%s: %v", key, cfg.ConsumerGroup, err)
					continue
				}

				streams[key] = struct{}{}
				log.Printf("discovered redis stream=%s group=%s start_id=%s", key, cfg.ConsumerGroup, startID)
			}
		}

		cursor = next
		if cursor == 0 {
			break
		}
	}

	for stream := range streams {
		if _, exists := discovered[stream]; !exists {
			delete(streams, stream)
			log.Printf("removed expired redis stream=%s", stream)
		}
	}
}

func processMessage(
	ctx context.Context,
	rdb *redis.Client,
	writeAPI api.WriteAPIBlocking,
	cfg Config,
	rules []Rule,
	stream string,
	msg redis.XMessage,
) {
	entry := normalize(msg.Values)
	if isIgnoredNamespace(entry["namespace"], cfg.IgnoredNamespaces) {
		ackMessage(ctx, rdb, cfg.ConsumerGroup, stream, msg.ID)
		return
	}

	match := detectMatch(entry, rules)
	if match == nil {
		ackMessage(ctx, rdb, cfg.ConsumerGroup, stream, msg.ID)
		return
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
			"redis_stream": stream,
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
		log.Printf("failed to write influx stream=%s id=%s; leaving message pending: %v", stream, msg.ID, err)
		return
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
	ackMessage(ctx, rdb, cfg.ConsumerGroup, stream, msg.ID)
}

func ackMessage(ctx context.Context, rdb *redis.Client, group, stream, messageID string) {
	if err := rdb.XAck(ctx, stream, group, messageID).Err(); err != nil {
		log.Printf("failed to acknowledge stream=%s id=%s group=%s: %v", stream, messageID, group, err)
	}
}

func claimPendingMessages(
	ctx context.Context,
	rdb *redis.Client,
	writeAPI api.WriteAPIBlocking,
	cfg Config,
	streams map[string]struct{},
	rules []Rule,
) {
	for stream := range streams {
		start := "0-0"
		for {
			messages, next, err := rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
				Stream:   stream,
				Group:    cfg.ConsumerGroup,
				Consumer: cfg.ConsumerName,
				MinIdle:  cfg.ClaimMinIdle,
				Start:    start,
				Count:    100,
			}).Result()
			if err != nil {
				if err != redis.Nil {
					log.Printf("xautoclaim error stream=%s group=%s: %v", stream, cfg.ConsumerGroup, err)
				}
				break
			}

			for _, msg := range messages {
				log.Printf("reclaimed pending message stream=%s id=%s consumer=%s", stream, msg.ID, cfg.ConsumerName)
				processMessage(ctx, rdb, writeAPI, cfg, rules, stream, msg)
			}

			if next == "0-0" || next == start {
				break
			}
			start = next
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

func hostname() string {
	value, err := os.Hostname()
	if err != nil || strings.TrimSpace(value) == "" {
		return "log-classifier"
	}
	return value
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
