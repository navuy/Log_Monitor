import os
import re

BASE_DIR = "/home/u1/log_monitor/ml_pipeline/spark_logs"
OUTPUT_FILE = "/home/u1/log_monitor/ml_pipeline/cleaned_logs.txt"

timestamp_pattern = re.compile(r"^\d{2}/\d{2}/\d{2}\s\d{2}:\d{2}:\d{2}\s")

def clean_log_line(line: str) -> str:
    line = line.strip()
    line = timestamp_pattern.sub("", line)
    return line

def process_log_file(file_path: str):
    with open(file_path, "r", encoding="utf-8", errors="ignore") as f:
        for line in f:
            cleaned = clean_log_line(line)
            if cleaned:
                yield cleaned

def main():
    count = 0
    with open(OUTPUT_FILE, "w", encoding="utf-8") as out:
        for root, _, files in os.walk(BASE_DIR):
            for file in files:
                if file.endswith(".log"):
                    log_path = os.path.join(root, file)
                    for cleaned_line in process_log_file(log_path):
                        out.write(cleaned_line + "\n")
                        count += 1
    print(f"Cleaned {count} lines saved to {OUTPUT_FILE}")

if __name__ == "__main__":
    main()


