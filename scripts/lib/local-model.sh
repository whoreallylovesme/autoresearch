autoresearch_prepare_model() {
  model_dir=${AUTORESEARCH_MODEL_DIR:-/opt/models}
  model_file="$model_dir/qwen2.5-0.5b-instruct-q4_k_m.gguf"
  mkdir -p "$model_dir"
  if [ ! -f "$model_file" ]; then
    curl -fL --retry 3 --retry-delay 2 https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF/resolve/main/qwen2.5-0.5b-instruct-q4_k_m.gguf -o "$model_file"
  fi
  printf '%s  %s\n' '74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db' "$model_file" | sha256sum -c -
}
