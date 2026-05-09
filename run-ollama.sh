#!/bin/sh
# Launches mini-llm-gateway against a local Ollama server.
#
# Prereqs:
#   1. Ollama installed and running:  ollama serve
#   2. Model pulled at least once:     ollama pull llama3.2:1b
#
# Override the model by editing GATEWAY_DEFAULT_MODEL below, or by passing
# {"model": "..."} in the request body / picking a different model in the UI.

GATEWAY_PROVIDERS=fake,ollama \
GATEWAY_DEFAULT_PROVIDER=ollama \
GATEWAY_DEFAULT_MODEL=llama3.2:1b \
OLLAMA_BASE_URL=http://localhost:11434 \
go run ./cmd/server
