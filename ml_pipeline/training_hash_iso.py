import os
from datetime import datetime
from sklearn.feature_extraction.text import HashingVectorizer
from sklearn.ensemble import IsolationForest
from scipy.sparse import save_npz
import joblib

LOG_FILE = "cleaned_logs.txt"
OUTPUT_FOLDER = "hashing_iso_output"
os.makedirs(OUTPUT_FOLDER, exist_ok=True)


def timestamp():
    return datetime.now().strftime("%Y%m%d_%H%M%S")


def load_and_group_logs(file_path, group_size=5):
    with open(file_path, "r") as f:
        all_lines = [line.strip() for line in f if line.strip()]
    grouped = [' '.join(all_lines[i:i + group_size])
               for i in range(0, len(all_lines), group_size)]
    print(f"✅ Total grouped logs: {len(grouped)}")
    return grouped


def get_vectorizer():
    """Stateless HashingVectorizer."""
    return HashingVectorizer(
        n_features=2**18,        
        alternate_sign=False,     
        norm=None
    )


def train_model(grouped_logs):
    """Vectorize logs and train Isolation Forest."""
    print("Vectorizing logs using HashingVectorizer...")
    vectorizer = get_vectorizer()
    X = vectorizer.transform(grouped_logs)

    print("Training Isolation Forest...")
    iso = IsolationForest(
        n_estimators=200,
        contamination=0.05,
        random_state=42
    )
    iso.fit(X)

    print("Training complete.")
    return X, iso


def save_model(X, iso):
    """Save model and vectors with timestamp."""
    ts = timestamp()

    vec_path = os.path.join(OUTPUT_FOLDER, f"tf_vectors_{ts}.npz")
    save_npz(vec_path, X)

    model_path = os.path.join(OUTPUT_FOLDER, f"isolation_forest_{ts}.pkl")
    joblib.dump(iso, model_path)

    print(f"Saved training vectors → {vec_path}")
    print(f"Saved model → {model_path}")


if __name__ == "__main__":
    print("Loading logs...")
    grouped_logs = load_and_group_logs(LOG_FILE)

    X, iso = train_model(grouped_logs)
    save_model(X, iso)

    print("\nTraining completed successfully!")

