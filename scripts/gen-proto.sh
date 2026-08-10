#!/bin/sh
set -e

# Ensure output directories exist
mkdir -p services/backend-api/pkg/pb/ccxt
mkdir -p services/backend-api/pkg/pb/telegram
mkdir -p services/telegram-service/proto
mkdir -p services/ccxt-service/proto

# Find ts-proto plugin (capture the lookup failure instead of aborting on it)
if ! PLUGIN_PATH=$(command -v protoc-gen-ts_proto); then
  PLUGIN_PATH=""
fi
if [ -z "$PLUGIN_PATH" ]; then
  # Fallback to common locations if not in PATH
  if [ -f "/usr/local/bin/protoc-gen-ts_proto" ]; then
    PLUGIN_PATH="/usr/local/bin/protoc-gen-ts_proto"
  elif [ -f "/usr/bin/protoc-gen-ts_proto" ]; then
    PLUGIN_PATH="/usr/bin/protoc-gen-ts_proto"
  else
    echo "Error: protoc-gen-ts_proto not found in PATH"
    exit 1
  fi
fi

echo "Using ts-proto plugin at: $PLUGIN_PATH"

echo "Generating Go code for CCXT..."
protoc --proto_path=protos \
  --go_out=services/backend-api/pkg/pb/ccxt --go_opt=paths=source_relative \
  --go-grpc_out=services/backend-api/pkg/pb/ccxt --go-grpc_opt=paths=source_relative \
  protos/ccxt_service.proto

echo "Generating Go code for Telegram..."
protoc --proto_path=protos \
  --go_out=services/backend-api/pkg/pb/telegram --go_opt=paths=source_relative \
  --go-grpc_out=services/backend-api/pkg/pb/telegram --go-grpc_opt=paths=source_relative \
  protos/telegram_service.proto

echo "Generating TS code for Telegram..."
protoc --proto_path=protos \
  --plugin=protoc-gen-ts_proto=$PLUGIN_PATH \
  --ts_proto_out=services/telegram-service/proto \
  --ts_proto_opt=outputServices=grpc-js,esModuleInterop=true,env=node \
  protos/telegram_service.proto

echo "Generating TS code for CCXT..."
protoc --proto_path=protos \
  --plugin=protoc-gen-ts_proto=$PLUGIN_PATH \
  --ts_proto_out=services/ccxt-service/proto \
  --ts_proto_opt=outputServices=grpc-js,esModuleInterop=true,env=node \
  protos/ccxt_service.proto

echo "Done!"
