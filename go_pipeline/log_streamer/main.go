package main

import (
	"bufio"
	"context"
	"fmt"
	//"io"
	"log"
	"os"
	"time"

	//"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/redis/go-redis/v9"
)

type LogCollector struct {
	dockerClient *client.Client
	redisClient  *redis.Client
	ctx          context.Context
}

func main() {
	ctx := context.Background()

	// Initialize Docker client
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal("Failed to create Docker client:", err)
	}
	defer dockerClient.Close()

	// Initialize Redis client
	redisAddr := getEnv("REDIS_ADDR", "redis:6379")
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	// Test Redis connection
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatal("Failed to connect to Redis:", err)
	}

	collector := &LogCollector{
		dockerClient: dockerClient,
		redisClient:  redisClient,
		ctx:          ctx,
	}

	log.Println("Starting log collector...")
	collector.watchContainers()
}

func (lc *LogCollector) watchContainers() {
	tracked := make(map[string]bool)

	for {
		containers, err := lc.dockerClient.ContainerList(lc.ctx, container.ListOptions{})
		if err != nil {
			log.Println("Error listing containers:", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, c := range containers {
			if !tracked[c.ID] {
				tracked[c.ID] = true
				go lc.streamContainerLogs(c.ID, c.Names[0])
				log.Printf("Started watching container: %s (%s)\n", c.Names[0], c.ID[:12])
			}
		}

		time.Sleep(10 * time.Second)
	}
}

func (lc *LogCollector) streamContainerLogs(containerID, containerName string) {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
		Tail:       "0",
	}

	reader, err := lc.dockerClient.ContainerLogs(lc.ctx, containerID, options)
	if err != nil {
		log.Printf("Error getting logs for %s: %v\n", containerID[:12], err)
		return
	}
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
	
		if len(line) > 8 {
			line = line[8:]
		}

		if line == "" {
			continue
		}

		if err := lc.pushToRedis(containerID, containerName, line); err != nil {
			log.Printf("Error pushing to Redis: %v\n", err)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading logs for %s: %v\n", containerID[:12], err)
	}
}

func (lc *LogCollector) pushToRedis(containerID, containerName, logLine string) error {
	streamKey := fmt.Sprintf("logs:%s", containerID)
	
	return lc.redisClient.XAdd(lc.ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"container_id":   containerID,
			"container_name": containerName,
			"log":            logLine,
			"timestamp":      time.Now().Unix(),
		},
	}).Err()
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
