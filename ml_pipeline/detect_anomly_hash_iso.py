import os
import numpy as np
import joblib
from sklearn.feature_extraction.text import HashingVectorizer

LOG_FILE = "sample_logs.txt"
MODEL_PATH = "hashing_iso_output/latest_model.pkl"
OUTPUT_FILE = "anomaly_results.csv"
GROUP_SIZE = 5


def load_logs(file_path):
    with open(file_path, "r", encoding="utf-8") as f:
        return [line.strip() for line in f if line.strip()]


def group_logs(logs, size):
    return [' '.join(logs[i:i + size]) for i in range(0, len(logs), size)]


def get_vectorizer():
    return HashingVectorizer(
        n_features=2**18,
        alternate_sign=False,
        norm=None
    )


def main():
    print("Loading Isolation Forest model...")
    model = joblib.load(MODEL_PATH)

    print("Loading and grouping logs...")
    logs = load_logs(LOG_FILE)
    grouped_logs = group_logs(logs, GROUP_SIZE)
    print(f"Total log groups formed: {len(grouped_logs)}")

    vectorizer = get_vectorizer()
    results = []

    print("\nPredicting anomalies...")
    for i, group_text in enumerate(grouped_logs):
        X = vectorizer.transform([group_text])
        score = model.decision_function(X)[0]
        label = model.predict(X)[0]

        results.append((i, score, label))

        print(f"Group {i+1:03d}: Score={score:.4f}  → {'⚠️ Anomaly' if label == -1 else 'Normal'}")

    print("\nSaving results to anomaly_results.csv...")
    with open(OUTPUT_FILE, "w", encoding="utf-8") as f:
        f.write("group_index,score,label\n")
        for idx, score, label in results:
            f.write(f"{idx},{score:.6f},{label}\n")

    print(f"Results saved → {OUTPUT_FILE}")

    anomalies = [r for r in results if r[2] == -1]
    print(f"\n⚠️ Found {len(anomalies)} anomalous groups\n")

    for idx, score, _ in anomalies:
        start = idx * GROUP_SIZE
        end = min(start + GROUP_SIZE, len(logs))
        print(f"\nGroup {idx + 1} (score={score:.4f})")
        for line in logs[start:end]:
            print("  " + line)
        print("-" * 80)


if __name__ == "__main__":
    main()

