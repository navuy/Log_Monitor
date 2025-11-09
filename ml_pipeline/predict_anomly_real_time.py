import os
import time
import numpy as np
import joblib
import redis
from datetime import datetime
from sklearn.feature_extraction.text import TfidfVectorizer


REDIS_HOST = os.getenv("REDIS_HOST", "localhost")
REDIS_PORT = int(os.getenv("REDIS_PORT", 6379))
CONTAINER_ID = "bbc76fe1a5770d865cc52074a1ea8a63a7846dcd14213629fdbca9f80514d817"
STREAM_KEY = f"logs:{CONTAINER_ID}"

MODEL_PATH = "tfidf_output_sklearn/isolation_forest_sklearn.pkl"
VECTORIZER_PATH = "tfidf_output_sklearn/tfidf_vectorizer_sklearn.pkl"
GROUP_SIZE = 5
ANOMALY_THRESHOLD = -0.008

CONSUMER_GROUP = "anomaly-detector-group"
CONSUMER_NAME = "detector-1"


def connect_redis():
    return redis.Redis(
        host=REDIS_HOST,
        port=REDIS_PORT,
        decode_responses=True
    )


def create_consumer_group(redis_client, stream_key, group_name):
    try:
        redis_client.xgroup_create(stream_key, group_name, id='0', mkstream=True)
    except redis.exceptions.ResponseError as e:
        if "BUSYGROUP" not in str(e):
            raise


def monitor_stream_realtime(redis_client, vectorizer, model):
    
    log_buffer = []
    group_counter = 0
    last_id = '>'
    
    print(f"[{datetime.now().strftime('%Y-%m-%d %H:%M:%S')}] Monitoring started for {STREAM_KEY}")
    print("-" * 80)
    
    while True:
        try:
            messages = redis_client.xreadgroup(
                CONSUMER_GROUP,
                CONSUMER_NAME,
                {STREAM_KEY: last_id},
                count=10,
                block=5000
            )
            
            if not messages:
                continue
            
            for stream, entries in messages:
                for entry_id, fields in entries:
                    log_text = fields.get('log', '')
                    
                    if log_text:
                        log_buffer.append(log_text)
                        
                        if len(log_buffer) >= GROUP_SIZE:
                            group_counter += 1
                            detect_anomaly_in_group(
                                log_buffer[:GROUP_SIZE],
                                group_counter,
                                vectorizer,
                                model
                            )
                            log_buffer = log_buffer[GROUP_SIZE:]
                        
                        redis_client.xack(STREAM_KEY, CONSUMER_GROUP, entry_id)
        
        except KeyboardInterrupt:
            print(f"\n[{datetime.now().strftime('%Y-%m-%d %H:%M:%S')}] Monitoring stopped")
            break
        except Exception as e:
            print(f"[{datetime.now().strftime('%Y-%m-%d %H:%M:%S')}] Error: {e}")
            time.sleep(1)


def detect_anomaly_in_group(logs, group_num, vectorizer, model):
    group_text = ' '.join(logs)
    
    X = vectorizer.transform([group_text])
    score = model.decision_function(X)[0]
    is_anomaly = score < ANOMALY_THRESHOLD
    
    if is_anomaly:
        timestamp = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
        print(f"\n[{timestamp}] ANOMALY DETECTED - Group #{group_num} (score={score:.4f})")
        print("-" * 80)
        for i, log in enumerate(logs, 1):
            print(f"{i}. {log}")
        print("-" * 80)


def main():
    redis_client = connect_redis()
    
    if not redis_client.exists(STREAM_KEY):
        print(f"Error: Stream {STREAM_KEY} does not exist")
        return
    
    vectorizer = joblib.load(VECTORIZER_PATH)
    model = joblib.load(MODEL_PATH)
    
    create_consumer_group(redis_client, STREAM_KEY, CONSUMER_GROUP)
    monitor_stream_realtime(redis_client, vectorizer, model)
    
    redis_client.close()


if __name__ == "__main__":
    main()
