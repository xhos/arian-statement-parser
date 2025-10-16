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


def extract_account_from_pdf(file_path: str) -> dict:
  """Auto-detect account type, number, and name from PDF content"""
  from app.utils import read_pdf
  import re

  result = {
    "type": None,
    "number": None,
    "name": None
  }

  try:
    # Read first page of PDF to check header
    pdf_text = read_pdf(file_path)[:3000]  # First 3000 chars should contain all header info

    # Detect account type
    if "personal savings account statement" in pdf_text.lower():
      result["type"] = "savings"
    elif "personal banking account statement" in pdf_text.lower():
      result["type"] = "chequing"
    elif "visa" in pdf_text.lower() or "credit card" in pdf_text.lower():
      result["type"] = "visa"

    # Extract account number (for chequing/savings)
    if match := re.search(r'account number[:\s]+([0-9-]+)', pdf_text, re.IGNORECASE):
      result["number"] = match.group(1)

    # Extract account name (for chequing/savings)
    # Look for lines like "RBC Advantage Banking 05172-5163878" or "RBC High Interest eSavingsTM 05172-5162458"
    if match := re.search(r'(RBC [^\n]+?)\s+\d{5}-\d{7}', pdf_text, re.IGNORECASE):
      account_name = match.group(1).strip()
      # Clean up the name
      account_name = re.sub(r'TM$', '', account_name)  # Remove trailing TM
      result["name"] = account_name

    # For VISA, extract last 4 digits
    if result["type"] == "visa":
      if match := re.search(r'(\d{4})\s+\d{2}\*\*\s+\*\*\*\*\s+(\d{4})', pdf_text):
        result["number"] = match.group(2)  # Last 4 digits
      # For VISA, we could use card type as name, but let's keep it simple
      result["name"] = "VISA"

  except Exception:
    pass

  return result


def extract_account_info(file_path: str) -> dict:
  """Extract account information from filename and PDF content"""
  import re

  filename = os.path.basename(file_path)

  # First, try to auto-detect from PDF content
  pdf_info = extract_account_from_pdf(file_path)

  account_type = pdf_info["type"]
  account_number = pdf_info["number"]
  account_name = pdf_info["name"]

  # If auto-detection fails, fall back to filename
  if not account_type:
    if 'visa' in filename.lower():
      account_type = "visa"
    else:
      # Extract first word to determine account type
      first_word = filename.split()[0].lower() if filename.split() else ""

      # Map first word to account types
      type_mapping = {
        "daily": "chequing",
        "savings": "savings",
        "student": "chequing",
        "chequing": "chequing",
      }

      account_type = type_mapping.get(first_word, "chequing")  # Default to chequing

  # If we didn't get account number from PDF, try filename
  # For VISA, extract last 4 digits from filename if present
  if not account_number:
    if account_type == "visa":
      # Look for 4-digit number at the start of filename
      if match := re.search(r'^(\d{4})', filename):
        account_number = match.group(1)
    else:
      # For chequing/savings, use first word of filename as identifier
      account_number = filename.split()[0] if filename.split() else None

  # If we didn't get account name from PDF, use a default based on type
  if not account_name:
    if account_type == "visa":
      account_name = f"VISA {account_number}" if account_number else "VISA"
    elif account_type == "savings":
      account_name = "Savings"
    else:
      account_name = "Chequing"


  return {
    "account_number": account_number,
    "account_type": account_type,
    "account_name": account_name
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
    tx["account_name"] = account_info["account_name"]
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
