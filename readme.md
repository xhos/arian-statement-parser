# arian-statement-parser

a more accurate name would be arian-rbc-statement-parser since only RBC statements are supported for now. this project is essentially a wrapper around [andrewscwei's amazing rbc-statement-parser](https://github.com/andrewscwei/rbc-statement-parser). i'd much prefer to handroll a pdf parser in Go but pdf is an extremely annoying format to work with, and only python has proper libs to work with it.

basically my wrapper provides a nice cli interface for parsing and inserts all the parsed statements directly into [ariand](https://github.com/xhos/ariand).

## usage

1. set up env's

```shell
cp .env.example .env
```

2. get the statements into the input directory

3. the first word in the .pdf files exactly coresponds the the name of the accounts you have created in ariand. For example transactions from `1234 Statement-1234 2024-04-08.pdf` would be parsed into the account with the name `1234`. If the account doesn't exist it will be created.

## Features

- Parses RBC PDF statements using the existing Python parser
- Uploads parsed transactions to Arian via gRPC API
- Configurable user and account settings

## Setup

1. Set your environment variables in `.env`:
   ```
   ARIAND_URL=api.arian.xhos.dev:443
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