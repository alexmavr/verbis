from huggingface_hub import snapshot_download
model_id="castorini/rank_zephyr_7b_v1_full"
snapshot_download(repo_id=model_id, local_dir="mxbai-rerank-hf",
                  local_dir_use_symlinks=False, revision="main")
