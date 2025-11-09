import os
import numpy as np
import joblib
from datetime import datetime
from sklearn.feature_extraction.text import TfidfVectorizer
from sklearn.ensemble import IsolationForest
from scipy.sparse import save_npz


log_file = "cleaned_logs.txt"
output_folder = "tfidf_output_sklearn"
os.makedirs(output_folder, exist_ok=True)


print("Loading log file...")
with open(log_file, "r") as f:
    all_lines = [line.strip() for line in f if line.strip()]

grouped_logs = [' '.join(all_lines[i:i+5]) for i in range(0, len(all_lines), 5)]
print(f"Total grouped logs: {len(grouped_logs)}")

print("Performing TF-IDF on CPU...")
vectorizer = TfidfVectorizer(max_features=5000)
tfidf_vectors = vectorizer.fit_transform(grouped_logs)

print(f"TF-IDF vector shape: {tfidf_vectors.shape}")

save_npz(os.path.join(output_folder, "tfidf_vectors_sklearn.npz"), tfidf_vectors)
np.save(os.path.join(output_folder, "feature_names_sklearn.npy"),
        np.array(vectorizer.get_feature_names_out()))
joblib.dump(vectorizer, os.path.join(output_folder, "tfidf_vectorizer_sklearn.pkl"))
print("Saved TF-IDF vectors & vectorizer.")


print("\nTraining Isolation Forest on CPU...")

iso_forest = IsolationForest(
    n_estimators=100,
    contamination=0.05,
    random_state=42
)

iso_forest.fit(tfidf_vectors)
print("Isolation Forest training complete.")

timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
model_filename = f"isolation_forest_{timestamp}.pkl"
joblib.dump(iso_forest, os.path.join(output_folder, model_filename))

print(f"💾 Saved Isolation Forest model → {model_filename}")
