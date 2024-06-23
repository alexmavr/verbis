import pandas as pd
from datasets import Dataset
import os
from ragas import evaluate
from ragas.metrics import faithfulness, answer_similarity, answer_correctness, answer_relevancy
import subprocess
# from ragas.metrics import context_precision, context_recall, context_relevancy

# Load the resulting eval_run_output file
input_file = "evals/eval_run_output.csv"
data = pd.read_csv(input_file)

# Convert all values to strings to avoid type errors
data = data.fillna('')  # Replace NaN values with empty strings
data = data.astype(str)

# Convert contexts from string representation of lists to actual lists
data['contexts'] = data['contexts'].apply(eval)

# Convert the DataFrame to the required format for evaluation
data_samples = {
    'question': data['question'].tolist(),
    'answer': data['answer'].tolist(),
    'contexts': data['contexts'].tolist(),
    'ground_truth': data['ground_truth'].tolist()
}

dataset = Dataset.from_dict(data_samples)

# Evaluate the dataset
score = evaluate(dataset, metrics=[faithfulness, answer_similarity, answer_correctness, answer_relevancy])

# The remaining metrics do not depend on the answer field, and should only be
# used to evaluate the ground truth dataset itself.
#score = evaluate(dataset, metrics=[faithfulness, answer_similarity, answer_correctness, answer_relevancy, context_precision, context_recall, context_relevancy])

print(score)
import json
with open('evals/scores.json', 'w') as f:
    f.write(json.dumps(score))

# Convert the score to a pandas DataFrame
score_df = score.to_pandas()

# Save the evaluation metrics to a new CSV file
output_file = "evals/evaluation_metrics_output.csv"
score_df.to_csv(output_file, index=False)

print(f"Evaluation metrics saved to {output_file}")


def get_pid(process_name):
    try:
        pid = subprocess.check_output(["pgrep", process_name]).decode().strip()
        return pid
    except subprocess.CalledProcessError:
        print(f"No process named '{process_name}' found.")
        return None

def get_memory_usage(pid):
    try:
        ps_output = subprocess.check_output(["ps", "-p", pid, "-o", "vsz,rss"]).decode().split("\n")
        if len(ps_output) > 1:
            vsz, rss = ps_output[1].split()
            vsz_mb = int(vsz) / 1024
            rss_mb = int(rss) / 1024
            return vsz_mb, rss_mb
        else:
            print(f"Could not retrieve memory usage for PID {pid}.")
            return None, None
    except subprocess.CalledProcessError:
        print(f"Failed to get memory usage for PID {pid}.")
        return None, None

process_name = "weaviate"
pid = get_pid(process_name)
if pid:
    vsz_mb, rss_mb = get_memory_usage(pid)
    if vsz_mb is not None and rss_mb is not None:
        print(f"Virtual Memory: {vsz_mb:.2f} MB")
        print(f"Physical Memory: {rss_mb:.2f} MB")