from huggingface_hub import snapshot_download
model_id="cross-encoder/ms-marco-MiniLM-L-12-v2"
snapshot_download(repo_id=model_id, local_dir="ms-marco-MiniLM-L-12-v2",
                  local_dir_use_symlinks=False, revision="main")
