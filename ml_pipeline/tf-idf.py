import os
import numpy as np
from sklearn.feature_extraction.text import TfidfVectorizer
from scipy.sparse import save_npz

log_file = "cleaned_logs.txt"

with open(log_file, "r") as f:
    all_lines = [line.strip() for line in f if line.strip()]

grouped_logs = []
for i in range(0, len(all_lines), 5):
    group = ' '.join(all_lines[i:i+5])
    grouped_logs.append(group)

print(f"Total grouped logs: {len(grouped_logs)}")
print("Sample grouped log:\n", grouped_logs[0])

vectorizer = TfidfVectorizer()
tfidf_vectors = vectorizer.fit_transform(grouped_logs)

print(f"TF-IDF vector shape: {tfidf_vectors.shape}")  
output_folder = "tfidf_output"
os.makedirs(output_folder, exist_ok=True)

save_npz(os.path.join(output_folder, "tfidf_vectors.npz"), tfidf_vectors)

np.save(os.path.join(output_folder, "feature_names.npy"), vectorizer.get_feature_names_out())

print(f"Saved TF-IDF vectors to {output_folder}/tfidf_vectors.npz")
print(f"Saved feature names to {output_folder}/feature_names.npy")
