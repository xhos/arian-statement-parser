import argparse
import json
import os
import sys

from app.chequing import is_chequing, parse_chequing
from app.entities import Config
from app.utils import format_transaction, write_file
from app.visa import is_visa, parse_visa


def parse_config(path: str) -> Config:
  try:
    with open(path, encoding="utf8") as json_file:
      return json.load(json_file)
  except Exception:
    return {}


def parse_files(path: str) -> list:
  files = []

  if os.path.isdir(path):
    files = [
      os.path.abspath(os.path.join(path, f))
      for f in os.listdir(path)
      if f.lower().endswith(".pdf")
    ]
  elif os.path.isfile(path) and path.lower().endswith(".pdf"):
    files = [os.path.abspath(path)]

  return files


def parse_args() -> tuple[list, dict, str, str]:
  parser = argparse.ArgumentParser(
    description="A script that parses RBC chequing and VISA statements in PDF format and extracts transactions"
  )

  parser.add_argument("path", help="Path or to PDF or directory of PDFs")
  parser.add_argument("--config", "-c", help="Path to config file", default=".rc")
  parser.add_argument("--out", "-o", help="Path to output file")
  parser.add_argument("--format", "-f", help="Output format", choices=["text", "json"], default="text")

  args = parser.parse_args()
  config = parse_config(args.config)
  files = parse_files(args.path)

  if len(files) == 0:
    print("No valid PDF files found in the specified directory.")
    sys.exit(1)

  return (files, config, args.out, args.format)


def extract_account_info(file_path: str) -> dict:
  """Extract account information from filename"""
  import re
  
  filename = os.path.basename(file_path)
  
  # Simple logic: if filename contains 'visa' anywhere, it's a visa statement
  # Otherwise, determine type from first word
  if 'visa' in filename.lower():
    account_type = "visa"
  else:
    # Extract first word to determine account type
    first_word = filename.split()[0].lower()
    
    # Map first word to account types
    type_mapping = {
      "daily": "chequing",
      "savings": "savings", 
      "student": "chequing",
      "chequing": "chequing",
    }
    
    account_type = type_mapping.get(first_word, "chequing")  # Default to chequing
  
  # Extract account number - it's simply the first word before the first space
  account_number = filename.split()[0] if filename.split() else None
  
  # Debug print to stderr so it doesn't break JSON parsing  
  print(f"DEBUG: filename='{filename}', account_number='{account_number}', account_type='{account_type}'", file=sys.stderr)
  
  return {
    "account_number": account_number,
    "account_type": account_type,
    "raw_type": filename.split()[0] if filename.split() else None
  }


def parse_pdf(file_path: str, categories: dict, excludes: list) -> list:
  account_info = extract_account_info(file_path)
  
  if is_chequing(file_path):
    transactions = parse_chequing(file_path, categories, excludes)
  elif is_visa(file_path):
    transactions = parse_visa(file_path, categories, excludes)
  else:
    return []
  
  # Add account info and source file to each transaction
  for tx in transactions:
    tx["account_number"] = account_info["account_number"] 
    tx["account_type"] = account_info["account_type"]
    tx["source_file"] = file_path
  
  return transactions


def main():
  files, config, out_file, output_format = parse_args()
  
  # Parse transactions and track file processing
  file_results = []
  transactions = []
  
  for file in files:
    file_transactions = parse_pdf(file, config.get("categories"), config.get("excludes"))
    file_results.append({
      "file": file,
      "transaction_count": len(file_transactions),
      "processed": len(file_transactions) > 0
    })
    transactions.extend(file_transactions)
  
  # Sort all transactions by date
  transactions = sorted(transactions, key=lambda tx: tx["date"])
  
  if output_format == "json":
    # Convert datetime objects to strings for JSON serialization
    json_transactions = []
    for tx in transactions:
      json_tx = dict(tx)
      json_tx["date"] = tx["date"].isoformat()
      json_tx["posting_date"] = tx["posting_date"].isoformat()
      json_transactions.append(json_tx)
    
    # Include file processing metadata
    result = {
      "transactions": json_transactions,
      "file_results": file_results,
      "summary": {
        "total_files": len(files),
        "processed_files": sum(1 for fr in file_results if fr["processed"]),
        "total_transactions": len(transactions)
      }
    }
    
    out_str = json.dumps(result, indent=2)
  else:
    out_str = "\n".join(
      format_transaction(
        tx,
        template=config.get("format") or "{date}\t{method}\t{code}\t{description}\t{category}\t{amount}",
        default_category="Other",
        padding=True,
      )
      for tx in transactions
    )

  if out_file:
    write_file(out_str, out_file)

  print(out_str)
  if output_format != "json":
    print()
    print(f"Parsing statements... OK: {len(transactions)} transaction(s)")


if __name__ == "__main__":
  main()
