import os
from llama_index.core import SimpleDirectoryReader
from ragas.testset import TestsetGenerator
from ragas.testset.generator import TestsetGenerator
from ragas.testset.evolutions import simple, reasoning, multi_context
from langchain_openai import ChatOpenAI, OpenAIEmbeddings

# load documents
print(f"Loading documents")
reader = SimpleDirectoryReader("../arxiv_papers/",num_files_limit=100)
documents = reader.load_data()

# generator with openai models
generator_llm = ChatOpenAI(model="gpt-4o")
critic_llm = ChatOpenAI(model="gpt-4o")
embeddings = OpenAIEmbeddings()

generator = TestsetGenerator.from_langchain(
    generator_llm,
    critic_llm,
    embeddings
)

distributions = {
    simple: 0.5,
    multi_context: 0.4,
    reasoning: 0.1
}

# generate testset
print(f"Generating testset")
testset = generator.generate_with_llamaindex_docs(documents, 100,distributions)
testset_df = testset.to_pandas()
output_file = "testset_output.csv"
testset_df.to_csv(output_file, index=False)

print(f"Testset saved to {output_file}")