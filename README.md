# Adaptive Log Anomaly Detection System

## Project Description

An intelligent log monitoring platform that automatically detects anomalies in Docker container logs using machine learning with continuous adaptive learning. The system combines real-time stream processing with periodic model retraining to evolve alongside changing application behavior.

### What It Does

This system monitors logs from Docker containers in real-time and identifies unusual patterns that may indicate security threats, system failures, or application errors. Unlike traditional rule-based monitoring, it uses machine learning (Isolation Forest) to learn what "normal" looks like for each container and automatically adapts as your applications evolve.

### Key Capabilities

**Adaptive Learning**: Models retrain periodically on historical data, continuously improving accuracy and reducing false positives as they learn your application's normal behavior patterns.

**Real-Time Detection**: Sub-second anomaly detection using Redis Streams for immediate awareness of potential issues.

**Container-Specific Intelligence**: Each container gets its own dedicated model, ensuring anomalies are detected based on that specific application's context rather than generic rules.

**Scalable Architecture**: Go-based log collection handles thousands of logs per second with concurrent processing, while Python handles the ML workload.

**Zero Downtime Updates**: Models retrain and update automatically without interrupting real-time monitoring.

### How It Works

1. **Log Collection**: Go service tails Docker container logs via Docker socket and streams them to Redis
2. **Persistence**: Logs are stored in InfluxDB time-series database for historical analysis
3. **Training**: Python service periodically queries InfluxDB, trains Isolation Forest models on historical data using TF-IDF vectorization
4. **Prediction**: Separate Python service consumes live logs from Redis Streams, vectorizes them, and scores them against the latest trained model
5. **Alerting**: Anomalies exceeding configurable thresholds are immediately flagged for investigation

### Technology Stack

- **Go** - High-performance log collection and streaming
- **Python** - Machine learning training and inference
- **Redis Streams** - Real-time message streaming with consumer groups
- **InfluxDB** - Time-series storage for historical logs
- **scikit-learn** - Isolation Forest anomaly detection
- **Docker Swarm** - Container orchestration

### Why This Approach

Traditional log monitoring relies on predefined rules and thresholds that quickly become outdated as applications change. This system learns what's normal for each application and adapts automatically, reducing alert fatigue while catching genuine anomalies that fixed rules would miss.

The adaptive retraining ensures the system improves over time - as your applications evolve, the models evolve with them, maintaining high accuracy without manual rule updates.

