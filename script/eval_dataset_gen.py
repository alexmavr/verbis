import os
from llama_index.core import SimpleDirectoryReader
from ragas.testset import TestsetGenerator
from ragas.testset.generator import TestsetGenerator
from ragas.testset.evolutions import simple, reasoning, multi_context
from langchain_openai import ChatOpenAI, OpenAIEmbeddings
from langchain.text_splitter import TokenTextSplitter
from ragas.testset.docstore import InMemoryDocumentStore
from ragas.testset.extractor import KeyphraseExtractor
from ragas.llms import LangchainLLMWrapper
from ragas.embeddings.base import LangchainEmbeddingsWrapper

num_tests = 500
num_files = 400
chunk_size = 2000

# load documents
print(f"Loading documents")
reader = SimpleDirectoryReader("../arxiv_papers/",num_files_limit=num_files)
documents = reader.load_data()

# generator with openai models
generator_llm = ChatOpenAI(model="gpt-4o")
critic_llm = ChatOpenAI(model="gpt-4o")
embeddings = OpenAIEmbeddings()


generator_llm_model = LangchainLLMWrapper(generator_llm)
embeddings_model = LangchainEmbeddingsWrapper(embeddings)
keyphrase_extractor = KeyphraseExtractor(llm=generator_llm_model)
splitter = TokenTextSplitter(chunk_size=chunk_size, chunk_overlap=0, disallowed_special=())
docstore = InMemoryDocumentStore(
    splitter=splitter,
    embeddings=embeddings_model,
    extractor=keyphrase_extractor,
)

generator = TestsetGenerator.from_langchain(
    generator_llm,
    critic_llm,
    embeddings,
    docstore=docstore,
)

distributions = {
    simple: 0.5,
    multi_context: 0.4,
    reasoning: 0.1
}

# generate testset
print(f"Generating testset")
testset = generator.generate_with_llamaindex_docs(documents, num_tests, distributions)
testset_df = testset.to_pandas()

testset_df = testset_df.dropna(subset=["ground_truth"])
output_file = "evals/testset_output.csv"
testset_df.to_csv(output_file, index=False)

print(f"Testset saved to {output_file}")