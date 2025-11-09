package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"
)

func main() {
	logFile := getEnv("LOGFILE", "/logs/cleaned_logs.txt")
	speed, err := strconv.ParseFloat(getEnv("SPEED", "1"), 64)
	if err != nil {
		log.Fatal("Invalid SPEED value")
	}

	delay := time.Duration(float64(time.Second) / speed)

	fmt.Printf("Streaming logs from: %s\n", logFile)
	fmt.Printf("Speed: %.2f logs/second\n", speed)

	for {
		if err := streamFile(logFile, delay); err != nil {
			log.Fatalf("Error: %v", err)
		}
	}
}

func streamFile(filename string, delay time.Duration) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
		time.Sleep(delay)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

