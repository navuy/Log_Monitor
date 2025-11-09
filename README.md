# Log_Monitor
Adaptive log anomaly detection using continuous model retraining. Go collector streams Docker logs to Redis and InfluxDB. Python ML pipeline periodically retrains container-specific Isolation Forest models on historical data, automatically updating predictions to adapt to evolving log patterns and reduce false positives.
