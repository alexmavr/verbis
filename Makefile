.PHONY: build verbis macapp 

SHELL=/bin/zsh

WEAVIATE_VERSION := v1.25.0
OLLAMA_VERSION := v0.1.32
DIST_DIR := ./dist
TMP_DIR := /tmp/weaviate-installation
ZIP_FILE := weaviate-$(WEAVIATE_VERSION)-darwin-all.zip
OLLAMA_BIN := ollama-darwin
OLLAMA_URL := https://github.com/ollama/ollama/releases/download/$(OLLAMA_VERSION)/$(OLLAMA_BIN)

# Builder environment
PYTHON_VERSION := 3.12
VENV_NAME := verbis-dev

PACKAGE := main

include .build.env

LDFLAGS := -X "$(PACKAGE).PosthogAPIKey=$(POSTHOG_PERSONAL_API_KEY)"

all: macapp

dist/ollama:
	# Ensure the distribution directory exists
	mkdir -p $(DIST_DIR)

	# Download the Ollama binary from GitHub
	curl -L $(OLLAMA_URL) -o $(DIST_DIR)/ollama

	# Make the binary executable
	chmod +x $(DIST_DIR)/ollama

dist/weaviate:
	# Ensure dist directory exists
	mkdir -p $(DIST_DIR)

	# Create a temporary directory for installation
	mkdir -p $(TMP_DIR)

	# Download the Weaviate zip file into the temporary directory
	curl -L https://github.com/weaviate/weaviate/releases/download/$(WEAVIATE_VERSION)/$(ZIP_FILE) -o $(TMP_DIR)/$(ZIP_FILE)

	# Unzip the downloaded file
	unzip $(TMP_DIR)/$(ZIP_FILE) -d $(TMP_DIR)

	# Move the weaviate binary to the dist directory
	mv $(TMP_DIR)/weaviate $(DIST_DIR)/weaviate

	# Remove the temporary directory and the zip file
	rm -rf $(TMP_DIR)

dist/pdftotext:
	brew install poppler
	mkdir -p dist/pdftotext
	mkdir -p dist/lib
	cp /opt/homebrew/bin/pdftotext dist/pdftotext/pdftotext
	cp /opt/homebrew/lib/libpoppler.136.dylib dist/lib/libpoppler.136.dylib

dist/rerank:
	( \
		export PATH="$(pyenv root)/shims:$(PATH)"; \
		source ~/.zshrc || true; \
		eval "$$(pyenv init --path)"; \
		eval "$$(pyenv init -)"; \
		eval "$$(pyenv virtualenv-init -)"; \
		pyenv activate $(VENV_NAME); \
		python3 -OO -m PyInstaller --onedir script/rerank.py --specpath dist/ \
	)

verbis: dist/rerank dist/weaviate dist/ollama dist/pdftotext
	# Ensure dist directory exists
	mkdir -p $(DIST_DIR)
	# Modelfile is needed for any custom model execution
	cp Modelfile.* dist/

	echo "$(LDFLAGS)"
	pushd verbis && go build -ldflags="$(LDFLAGS)" -o ../$(DIST_DIR)/verbis . && popd

macapp: verbis dist/ollama dist/weaviate dist/rerank
	pushd macapp && npm install && npm run package && popd

builder-env:
	brew install pyenv pyenv-virtualenv
	pyenv install -s $(PYTHON_VERSION)
	# Properly initialize pyenv and pyenv-virtualenv in a subshell
	( \
		export PATH="$(pyenv root)/shims:$(PATH)"; \
		source ~/.zshrc || true; \
		eval "$$(pyenv init --path)"; \
		eval "$$(pyenv init -)"; \
		eval "$$(pyenv virtualenv-init -)"; \
		pyenv virtualenv $(PYTHON_VERSION) $(VENV_NAME); \
		pyenv activate $(VENV_NAME); \
		pip install --upgrade pip; \
		pip install poetry; \
		cd script; \
		poetry install; \
	)

clean:
	rm dist/weaviate dist/ollama dist/verbis
	rm -r dist/rerank build/*

kill:
	pkill -9 weaviate ollama verbis
