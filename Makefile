.PHONY: build verbis macapp 

SHELL=/bin/zsh

VERSION := v0.0.0
TAG := $(shell git describe --tags --always --dirty)
WEAVIATE_VERSION := v1.25.5
OLLAMA_VERSION := v0.1.46
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

LDFLAGS := \
    -X "$(PACKAGE).Version=$(VERSION)" \
    -X "$(PACKAGE).Tag=$(TAG)" \
    -X "$(PACKAGE).PosthogAPIKey=$(POSTHOG_PERSONAL_API_KEY)" \
    -X "$(PACKAGE).AzureSecretID=$(AZURE_SECRET_ID)" \
    -X "$(PACKAGE).AzureSecretValue=$(AZURE_SECRET_VALUE)" \
    -X "$(PACKAGE).SlackClientID=$(SLACK_CLIENT_ID)" \
    -X "$(PACKAGE).SlackClientSecret=$(SLACK_CLIENT_SECRET)" \
    -X "$(PACKAGE).SlackSigningSecret=$(SLACK_SIGNING_SECRET)" \
    -X "$(PACKAGE).SlackBotToken=$(SLACK_BOT_TOKEN)"

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

dist/certs:
	mkdir -p $(DIST_DIR)/certs
	cp verbis/certs/* $(DIST_DIR)/certs

dist/ms-marco-MiniLM-L-12-v2:
	wget https://huggingface.co/prithivida/flashrank/resolve/main/ms-marco-MiniLM-L-12-v2.zip
	unzip -o ms-marco-MiniLM-L-12-v2.zip -d dist/
	pushd dist/ms-marco-MiniLM-L-12-v2 && \
		mv flashrank-MiniLM-L-12-v2_Q.onnx reranker.onnx && \
		popd
	rm ms-marco-MiniLM-L-12-v2.zip

dist/pdftotext:
	brew install poppler fontconfig
	mkdir -p dist/pdftotext
	mkdir -p dist/lib
	cp /opt/homebrew/lib/libpoppler.136.dylib dist/lib/libpoppler.136.dylib
	cp /opt/homebrew/opt/xz/lib/liblzma.5.dylib dist/lib/liblzma.5.dylib
	cp /opt/homebrew/opt/freetype/lib/libfreetype.6.dylib dist/lib/libfreetype.6.dylib
	cp /opt/homebrew/opt/fontconfig/lib/libfontconfig.1.dylib dist/lib/libfontconfig.1.dylib
	cp /opt/homebrew/opt/jpeg-turbo/lib/libjpeg.8.dylib dist/lib/libjpeg.8.dylib
	cp /opt/homebrew/opt/gpgme/lib/libgpgmepp.6.dylib dist/lib/libgpgmepp.6.dylib
	cp /opt/homebrew/opt/gpgme/lib/libgpgme.11.dylib dist/lib/libgpgme.11.dylib
	cp /opt/homebrew/opt/openjpeg/lib/libopenjp2.7.dylib dist/lib/libopenjp2.7.dylib
	cp /opt/homebrew/opt/little-cms2/lib/liblcms2.2.dylib dist/lib/liblcms2.2.dylib
	cp /opt/homebrew/opt/gettext/lib/libintl.8.dylib dist/lib/libintl.8.dylib
	cp /opt/homebrew/opt/libpng/lib/libpng16.16.dylib dist/lib/libpng16.16.dylib
	cp /opt/homebrew/opt/libtiff/lib/libtiff.6.dylib dist/lib/libtiff.6.dylib
	cp /opt/homebrew/opt/nss/lib/libnss3.dylib dist/lib/libnss3.dylib
	cp /opt/homebrew/opt/nss/lib/libnssutil3.dylib dist/lib/libnssutil3.dylib
	cp /opt/homebrew/opt/nss/lib/libsmime3.dylib dist/lib/libsmime3.dylib
	cp /opt/homebrew/opt/nss/lib/libssl3.dylib dist/lib/libssl3.dylib
	cp /opt/homebrew/opt/nspr/lib/libplds4.dylib dist/lib/libplds4.dylib
	cp /opt/homebrew/opt/nspr/lib/libplc4.dylib dist/lib/libplc4.dylib
	cp /opt/homebrew/opt/nspr/lib/libnspr4.dylib dist/lib/libnspr4.dylib
	sudo cp -L /opt/homebrew/opt/openjpeg/lib/libopenjp2.7.dylib dist/lib/libopenjp2.7.dylib
	cp /opt/homebrew/opt/libassuan/lib/libassuan.0.dylib dist/lib/libassuan.0.dylib
	cp /opt/homebrew/opt/zstd/lib/libzstd.1.dylib dist/lib/libzstd.1.dylib
	cp /opt/homebrew/opt/libgpg-error/lib/libgpg-error.0.dylib dist/lib/libgpg-error.0.dylib 
	cp /opt/homebrew/bin/pdftotext dist/pdftotext/pdftotext
	install_name_tool -change /opt/homebrew/opt/gettext/lib/libintl.8.dylib @executable_path/../lib/libintl.8.dylib dist/lib/libgpg-error.0.dylib
	install_name_tool -change /opt/homebrew/opt/xz/lib/liblzma.5.dylib @executable_path/../lib/liblzma.5.dylib dist/lib/libtiff.6.dylib
	install_name_tool -change /opt/homebrew/opt/zstd/lib/libzstd.1.dylib @executable_path/../lib/libzstd.1.dylib dist/lib/libtiff.6.dylib
	install_name_tool -change /opt/homebrew/opt/freetype/lib/libfreetype.6.dylib @executable_path/../lib/libfreetype.6.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/opt/fontconfig/lib/libfontconfig.1.dylib @executable_path/../lib/libfontconfig.1.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/opt/jpeg-turbo/lib/libjpeg.8.dylib @executable_path/../lib/libjpeg.8.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/opt/gpgme/lib/libgpgmepp.6.dylib @executable_path/../lib/libgpgmepp.6.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/opt/gpgme/lib/libgpgme.11.dylib @executable_path/../lib/libgpgme.11.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/opt/gpgme/lib/libgpgme.11.dylib @executable_path/../lib/libgpgme.11.dylib dist/lib/libgpgmepp.6.dylib
	install_name_tool -change /opt/homebrew/Cellar/gpgme/1.23.2_1/lib/libgpgme.11.dylib @executable_path/../lib/libgpgme.11.dylib dist/lib/libgpgmepp.6.dylib
	install_name_tool -change /opt/homebrew/opt/openjpeg/lib/libopenjp2.7.dylib @executable_path/../lib/libopenjp2.7.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/opt/little-cms2/lib/liblcms2.2.dylib @executable_path/../lib/liblcms2.2.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/opt/libpng/lib/libpng16.16.dylib @executable_path/../lib/libpng16.16.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/opt/libtiff/lib/libtiff.6.dylib @executable_path/../lib/libtiff.6.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/opt/nss/lib/libnss3.dylib @executable_path/../lib/libnss3.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/Cellar/nss/3.100/lib/libnssutil3.dylib @executable_path/../lib/libnssutil3.dylib dist/lib/libnss3.dylib
	install_name_tool -change /opt/homebrew/Cellar/nss/3.100/lib/libnssutil3.dylib @executable_path/../lib/libnssutil3.dylib dist/lib/libsmime3.dylib
	install_name_tool -change /opt/homebrew/opt/nss/lib/libnss3.dylib @executable_path/../lib/libnss3.dylib dist/lib/libsmime3.dylib
	install_name_tool -change /opt/homebrew/Cellar/nss/3.100/lib/libnss3.dylib @executable_path/../lib/libnss3.dylib dist/lib/libsmime3.dylib
	install_name_tool -change /opt/homebrew/opt/nss/lib/libnss3.dylib @executable_path/../lib/libnss3.dylib dist/lib/libssl3.dylib
	install_name_tool -change /opt/homebrew/Cellar/nss/3.100/lib/libnss3.dylib @executable_path/../lib/libnss3.dylib dist/lib/libssl3.dylib
	install_name_tool -change /opt/homebrew/Cellar/nss/3.100/lib/libnssutil3.dylib @executable_path/../lib/libnssutil3.dylib dist/lib/libssl3.dylib
	install_name_tool -change /opt/homebrew/opt/nss/lib/libnssutil3.dylib @executable_path/../lib/libnssutil3.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/opt/nss/lib/libnssutil3.dylib @executable_path/../lib/libnssutil3.dylib dist/lib/libnss3.dylib
	install_name_tool -change /opt/homebrew/Cellar/nss/3.100/lib/libnssutil3.dylib 	@executable_path/../lib/libnssutil3.dylib dist/lib/libnss3.dylib
	install_name_tool -change /opt/homebrew/opt/nss/lib/libsmime3.dylib @executable_path/../lib/libsmime3.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/opt/nss/lib/libssl3.dylib @executable_path/../lib/libssl3.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/opt/nspr/lib/libplds4.dylib @executable_path/../lib/libplds4.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/opt/nspr/lib/libplc4.dylib @executable_path/../lib/libplc4.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/opt/nspr/lib/libnspr4.dylib @executable_path/../lib/libnspr4.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/opt/nspr/lib/libnspr4.dylib @executable_path/../lib/libnspr4.dylib dist/lib/libplds4.dylib
	install_name_tool -change /opt/homebrew/Cellar/nspr/4.35/lib/libnspr4.dylib @executable_path/../lib/libnspr4.dylib dist/lib/libplds4.dylib
	install_name_tool -change /opt/homebrew/opt/nspr/lib/libnspr4.dylib @executable_path/../lib/libnspr4.dylib dist/lib/libplc4.dylib
	install_name_tool -change /opt/homebrew/Cellar/nspr/4.35/lib/libnspr4.dylib @executable_path/../lib/libnspr4.dylib dist/lib/libplc4.dylib
	install_name_tool -change /opt/homebrew/opt/libassuan/lib/libassuan.0.dylib  @executable_path/../lib/libassuan.0.dylib dist/lib/libpoppler.136.dylib
	install_name_tool -change /opt/homebrew/opt/libgpg-error/lib/libgpg-error.0.dylib @executable_path/../lib/libgpg-error.0.dylib dist/lib/libgpgme.11.dylib

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

verbis: dist/rerank dist/weaviate dist/ollama dist/pdftotext dist/ms-marco-MiniLM-L-12-v2 dist/certs
	@echo Building $(VERSION) $(TAG)
	# Ensure dist directory exists
	mkdir -p $(DIST_DIR)
	# Modelfile is needed for any custom model execution
	cp Modelfile.* dist/

	@pushd verbis && go build -ldflags="$(LDFLAGS)" -o ../$(DIST_DIR)/verbis . && popd

macapp: verbis 
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
		pip install setuptools; \
		cd script; \
		poetry install; \
	)

release: verbis
	pushd macapp && npm run make:sign && popd

clean:
	rm dist/weaviate dist/ollama dist/verbis || true
	rm -r dist/ms-marco-MiniLM-L-12-v2 || true
	rm -rf dist/rerank dist/pdftotext dist/lib || true

kill:
	pkill -9 weaviate ollama verbis
