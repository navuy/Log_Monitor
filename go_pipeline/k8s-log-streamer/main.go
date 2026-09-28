package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	RedisAddr         string
	LogDir            string
	DiscoveryInterval time.Duration
	ReadInterval      time.Duration
	StreamMaxLen      int64
	StreamIdleTTL     time.Duration
	StartFromEnd      bool
	KeyPrefix         string
}

type Collector struct {
	cfg         Config
	ctx         context.Context
	redisClient *redis.Client

	mu      sync.Mutex
	tracked map[string]bool
}

type LogFileMeta struct {
	PodName       string
	Namespace     string
	ContainerName string
	ContainerID   string
}

type LogEntry struct {
	Timestamp string
	Stream    string
	Tag       string
	Message   string
	Raw       string
}

func main() {
	cfg := loadConfig()
	ctx := context.Background()

	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
	})

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to connect to Redis at %s: %v", cfg.RedisAddr, err)
	}

	collector := &Collector{
		cfg:         cfg,
		ctx:         ctx,
		redisClient: redisClient,
		tracked:     make(map[string]bool),
	}

	log.Printf("starting Kubernetes log streamer; log_dir=%s redis=%s", cfg.LogDir, cfg.RedisAddr)
	collector.run()
}

func loadConfig() Config {
	return Config{
		RedisAddr:         getEnv("REDIS_ADDR", "redis:6379"),
		LogDir:            getEnv("LOG_DIR", "/var/log/containers"),
		DiscoveryInterval: getDurationEnv("DISCOVERY_INTERVAL", 10*time.Second),
		ReadInterval:      getDurationEnv("READ_INTERVAL", 1*time.Second),
		StreamMaxLen:      getInt64Env("STREAM_MAXLEN", 10000),
		StreamIdleTTL:     getDurationEnv("STREAM_IDLE_TTL", 24*time.Hour),
		StartFromEnd:      getBoolEnv("START_FROM_END", true),
		KeyPrefix:         getEnv("STREAM_KEY_PREFIX", "logs:k8s"),
	}
}

func (c *Collector) run() {
	for {
		if err := c.discoverLogFiles(); err != nil {
			log.Printf("log discovery error: %v", err)
		}

		time.Sleep(c.cfg.DiscoveryInterval)
	}
}

func (c *Collector) discoverLogFiles() error {
	pattern := filepath.Join(c.cfg.LogDir, "*.log")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to glob %s: %w", pattern, err)
	}

	for _, path := range matches {
		if !c.markTracked(path) {
			continue
		}

		go c.tailFile(path)
		log.Printf("started tailing %s", path)
	}

	return nil
}

func (c *Collector) markTracked(path string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tracked[path] {
		return false
	}

	c.tracked[path] = true
	return true
}

func (c *Collector) untrack(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tracked, path)
}

func (c *Collector) tailFile(path string) {
	defer c.untrack(path)

	meta, err := parseLogFileName(path)
	if err != nil {
		log.Printf("skipping %s: %v", path, err)
		return
	}

	file, err := os.Open(path)
	if err != nil {
		log.Printf("failed to open %s: %v", path, err)
		return
	}
	defer file.Close()

	if c.cfg.StartFromEnd {
		if _, err := file.Seek(0, io.SeekEnd); err != nil {
			log.Printf("failed to seek %s: %v", path, err)
			return
		}
	}

	reader := bufio.NewReader(file)
	var pending string
	var offset int64
	if offset, err = file.Seek(0, io.SeekCurrent); err != nil {
		log.Printf("failed to get current offset for %s: %v", path, err)
		return
	}

	for {
		chunk, readErr := reader.ReadString('\n')
		if len(chunk) > 0 {
			offset += int64(len(chunk))
			pending += chunk
		}

		if readErr == nil {
			line := strings.TrimRight(pending, "\r\n")
			pending = ""

			if line == "" {
				continue
			}

			entry := parseCRI(line)
			if err := c.pushToRedis(meta, entry); err != nil {
				log.Printf("failed to push log from %s: %v", path, err)
			}

			continue
		}

		if !errors.Is(readErr, io.EOF) {
			log.Printf("read error for %s: %v", path, readErr)
			return
		}

		stat, statErr := os.Stat(path)
		if statErr != nil {
			log.Printf("failed to stat %s: %v", path, statErr)
			return
		}

		if stat.Size() < offset {
			log.Printf("detected truncation for %s; reopening from start", path)
			if err := file.Close(); err != nil {
				log.Printf("failed to close truncated file %s: %v", path, err)
			}

			file, err = os.Open(path)
			if err != nil {
				log.Printf("failed to reopen %s: %v", path, err)
				return
			}
			defer file.Close()

			reader = bufio.NewReader(file)
			offset = 0
			pending = ""
		}

		time.Sleep(c.cfg.ReadInterval)
	}
}

func parseLogFileName(path string) (LogFileMeta, error) {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	parts := strings.SplitN(name, "_", 3)
	if len(parts) != 3 {
		return LogFileMeta{}, fmt.Errorf("unexpected Kubernetes log filename format")
	}

	containerPart := parts[2]
	lastDash := strings.LastIndex(containerPart, "-")
	if lastDash == -1 {
		return LogFileMeta{}, fmt.Errorf("missing container id segment")
	}

	return LogFileMeta{
		PodName:       parts[0],
		Namespace:     parts[1],
		ContainerName: containerPart[:lastDash],
		ContainerID:   containerPart[lastDash+1:],
	}, nil
}

func parseCRI(line string) LogEntry {
	parts := strings.SplitN(line, " ", 4)
	if len(parts) < 4 {
		return LogEntry{
			Message: line,
			Raw:     line,
		}
	}

	return LogEntry{
		Timestamp: parts[0],
		Stream:    parts[1],
		Tag:       parts[2],
		Message:   parts[3],
		Raw:       line,
	}
}

func (c *Collector) pushToRedis(meta LogFileMeta, entry LogEntry) error {
	streamKey := fmt.Sprintf(
		"%s:ns:%s:pod:%s:container:%s",
		c.cfg.KeyPrefix,
		sanitizeKeyPart(meta.Namespace),
		sanitizeKeyPart(meta.PodName),
		sanitizeKeyPart(meta.ContainerName),
	)

	_, err := c.redisClient.Pipelined(c.ctx, func(pipe redis.Pipeliner) error {
		pipe.XAdd(c.ctx, &redis.XAddArgs{
			Stream: streamKey,
			MaxLen: c.cfg.StreamMaxLen,
			Approx: true,
			Values: map[string]interface{}{
				"namespace":      meta.Namespace,
				"pod_name":       meta.PodName,
				"container_name": meta.ContainerName,
				"container_id":   meta.ContainerID,
				"log":            entry.Message,
				"raw":            entry.Raw,
				"stream":         entry.Stream,
				"tag":            entry.Tag,
				"timestamp":      entry.Timestamp,
				"ingested_at":    time.Now().UTC().Format(time.RFC3339Nano),
			},
		})

		if c.cfg.StreamIdleTTL > 0 {
			pipe.Expire(c.ctx, streamKey, c.cfg.StreamIdleTTL)
		}

		return nil
	})

	return err
}

func sanitizeKeyPart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "unknown"
	}

	replacer := strings.NewReplacer("/", "-", ":", "-", " ", "-", ".", "-", "_", "-")
	value = replacer.Replace(value)
	return value
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return defaultValue
	}

	value, err := time.ParseDuration(raw)
	if err == nil {
		return value
	}

	if seconds, err := strconv.Atoi(raw); err == nil {
		return time.Duration(seconds) * time.Second
	}

	log.Printf("invalid duration for %s=%q; using default %s", key, raw, defaultValue)
	return defaultValue
}

func getInt64Env(key string, defaultValue int64) int64 {
	raw := os.Getenv(key)
	if raw == "" {
		return defaultValue
	}

	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		log.Printf("invalid integer for %s=%q; using default %d", key, raw, defaultValue)
		return defaultValue
	}

	return value
}

func getBoolEnv(key string, defaultValue bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return defaultValue
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		log.Printf("invalid bool for %s=%q; using default %t", key, raw, defaultValue)
		return defaultValue
	}

	return value
}
