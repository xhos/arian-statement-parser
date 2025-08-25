# Arian Statement Parser

A Go application that integrates RBC statement parsing with the Arian financial management system.

## Features

- Parses RBC PDF statements using the existing Python parser
- Uploads parsed transactions to Arian via gRPC API
- Configurable user and account settings

## Setup

1. Set your environment variables in `.env`:
   ```
   GRPC_SERVER=api.arian.xhos.dev:443
   API_KEY=your_api_key_here
   USER_ID=your_user_id_here
   ```

2. Install dependencies:
   ```bash
   go mod tidy
   ```

## Usage

```bash
go run cmd/main.go -pdf /path/to/statements.pdf -user your_user_id
```

### Options

- `-pdf`: Path to PDF file or directory containing PDFs to parse (required)
- `-user`: User ID for Arian API (can be set via USER_ID env var)
- `-config`: Path to Python parser config file (optional)
- `-server`: gRPC server URL (default: api.arian.xhos.dev:443)
- `-key`: API key for authentication (can be set via API_KEY env var)

## Requirements

- Go 1.21+
- Python 3 with the RBC statement parser dependencies
- Access to Arian gRPC server