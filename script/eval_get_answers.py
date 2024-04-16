import pandas as pd
import requests
import json

# Load dataset from CSV
input_file = "evals/testset_output_small.csv"
data = pd.read_csv(input_file)

# Define the API endpoints
create_conversation_url = "http://localhost:8081/conversations"
prompt_url = "http://localhost:8081/conversations/{conversation_id}/prompt"

# Function to create a new conversation and get the conversation ID
def create_conversation():
    response = requests.post(create_conversation_url)
    if response.status_code == 200:
        conversation_id = response.json().get("id", "")
        if conversation_id:
            print(f"Created new conversation with ID: {conversation_id}")
        return conversation_id
    else:
        print(f"Error: Unable to create conversation. Received status code {response.status_code}")
        return ""

# Function to get the answer for a given question
def get_answer(question, conversation_id):
    payload = {
        "prompt": question
    }
    headers = {
        "Content-Type": "application/json"
    }

    response = requests.post(prompt_url.format(conversation_id=conversation_id), headers=headers, data=json.dumps(payload))

    if response.status_code == 200:
        # Collect streamed response
        response_content = ""
        for line in response.iter_lines():
            if line:
                json_response = json.loads(line.decode('utf-8'))
                if 'message' in json_response:
                    response_content += json_response['message'].get('content', '')
        print(f"Received response for conversation ID {conversation_id}")
        return response_content
    else:
        print(f"Error: Received status code {response.status_code} for conversation ID {conversation_id}")
        return ""

# Create a new column in the DataFrame for the answers
data["answer"] = ""

# Iterate over each row in the DataFrame
for index, row in data.iterrows():
    question = row["question"]
    print(f"Processing row {index + 1}/{len(data)}: Question: {question}")
    
    conversation_id = create_conversation()
    if conversation_id:
        answer = get_answer(question, conversation_id)
        data.at[index, "answer"] = answer
        print(f"Row {index + 1} processed successfully.\n")
    else:
        print(f"Failed to create conversation for row {index + 1}.\n")

# Save the updated DataFrame to a new CSV file
output_file = "evals/eval_run_output.csv"
data.to_csv(output_file, index=False)

print(f"Output saved to {output_file}")
