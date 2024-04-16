.PHONY: build service

all: macapp

service:
	pushd service && go build . && popd

macapp: service ollama
	pushd macapp && npm install && npm make && popd

ollama:
	// fetch latest ollama binary