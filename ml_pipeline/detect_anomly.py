import os
import numpy as np
import joblib
from sklearn.feature_extraction.text import TfidfVectorizer

LOG_FILE = "sample_logs.txt"
MODEL_PATH = "tfidf_output_sklearn/isolation_forest_sklearn.pkl"
VECTORIZER_PATH = "tfidf_output_sklearn/tfidf_vectorizer_sklearn.pkl"
OUTPUT_FILE = "anomaly_results.csv"
GROUP_SIZE = 5

def load_logs(file_path):
    with open(file_path, "r", encoding="utf-8") as f:
        return [line.strip() for line in f if line.strip()]

def group_logs(logs, size):
    return [' '.join(logs[i:i + size]) for i in range(0, len(logs), size)]

def main():
    print("Loading TF-IDF vectorizer and Isolation Forest model...")
    vectorizer = joblib.load(VECTORIZER_PATH)
    model = joblib.load(MODEL_PATH)

    print("Loading and grouping logs...")
    logs = load_logs(LOG_FILE)
    grouped_logs = group_logs(logs, GROUP_SIZE)
    print(f"Total log groups formed: {len(grouped_logs)}")

    results = []

    print("Predicting each group separately...")
    for i, group_text in enumerate(grouped_logs):
        X = vectorizer.transform([group_text])
        score = model.decision_function(X)[0]
        label = model.predict(X)[0]  

        results.append((i, score, label))

        print(f"Group {i+1:03d}: Score={score:.4f}  → {'⚠️ Anomaly' if score < -0.008 else 'Normal'}")

    print("\nSaving results to anomaly_results.csv...")
    with open(OUTPUT_FILE, "w", encoding="utf-8") as f:
        f.write("group_index,score,label\n")
        for i, score, label in results:
            f.write(f"{i},{score:.6f},{label}\n")

    print(f"Results saved → {OUTPUT_FILE}")

    anomalies = [r for r in results if r[1] < -0.008]
    print(f"\n⚠️  Found {len(anomalies)} anomalous groups\n")

    for idx, score, _ in anomalies:
        start = idx * GROUP_SIZE
        end = min(start + GROUP_SIZE, len(logs))
        print(f"Group {idx + 1} (score={score:.4f})")
        for line in logs[start:end]:
            print("  " + line)
        print("-" * 80)

if __name__ == "__main__":
    main()
