#!/usr/bin/env python3
"""
NebulaIO S3 Select Example

Demonstrates running SQL queries on CSV, JSON, and Parquet files
without downloading the entire object.
"""

import os
import boto3
from botocore.config import Config

# Configuration
# Note: Set credentials via environment variables (no defaults for security)
ENDPOINT = os.getenv("NEBULAIO_ENDPOINT", "https://localhost:9000")
ACCESS_KEY = os.getenv("NEBULAIO_ACCESS_KEY", "admin")
SECRET_KEY = os.getenv("NEBULAIO_SECRET_KEY")  # Required - set via environment

if not SECRET_KEY:
    raise ValueError("NEBULAIO_SECRET_KEY environment variable is required")


def get_s3_client():
    """Create an S3 client configured for NebulaIO."""
    return boto3.client(
        "s3",
        endpoint_url=ENDPOINT,
        aws_access_key_id=ACCESS_KEY,
        aws_secret_access_key=SECRET_KEY,
        config=Config(signature_version="s3v4"),
        region_name="us-east-1",
    )


def select_from_csv(bucket: str, key: str, query: str):
    """
    Run an S3 Select query on a CSV file.

    Example CSV format:
    id,name,age,city,salary
    1,John,30,NYC,75000
    2,Jane,25,LA,80000
    """
    client = get_s3_client()

    response = client.select_object_content(
        Bucket=bucket,
        Key=key,
        Expression=query,
        ExpressionType="SQL",
        InputSerialization={
            "CSV": {
                "FileHeaderInfo": "USE",  # Use first row as column names
                "FieldDelimiter": ",",
                "RecordDelimiter": "\n",
            },
            "CompressionType": "NONE",  # Or "GZIP", "BZIP2"
        },
        OutputSerialization={"JSON": {"RecordDelimiter": "\n"}},
    )

    # Process streaming response
    results = []
    for event in response["Payload"]:
        if "Records" in event:
            results.append(event["Records"]["Payload"].decode("utf-8"))
        elif "Stats" in event:
            stats = event["Stats"]["Details"]
            print(f"Bytes scanned: {stats['BytesScanned']}")
            print(f"Bytes processed: {stats['BytesProcessed']}")
            print(f"Bytes returned: {stats['BytesReturned']}")

    return "".join(results)


def select_from_json(bucket: str, key: str, query: str):
    """
    Run an S3 Select query on a JSON Lines file.

    Example JSON Lines format:
    {"id": 1, "name": "John", "age": 30, "city": "NYC"}
    {"id": 2, "name": "Jane", "age": 25, "city": "LA"}
    """
    client = get_s3_client()

    response = client.select_object_content(
        Bucket=bucket,
        Key=key,
        Expression=query,
        ExpressionType="SQL",
        InputSerialization={
            "JSON": {"Type": "LINES"},  # Or "DOCUMENT" for single JSON
            "CompressionType": "NONE",
        },
        OutputSerialization={"JSON": {"RecordDelimiter": "\n"}},
    )

    results = []
    for event in response["Payload"]:
        if "Records" in event:
            results.append(event["Records"]["Payload"].decode("utf-8"))

    return "".join(results)


def select_from_parquet(bucket: str, key: str, query: str):
    """
    Run an S3 Select query on a Parquet file.

    Parquet is self-describing, so no additional input options needed.
    """
    client = get_s3_client()

    response = client.select_object_content(
        Bucket=bucket,
        Key=key,
        Expression=query,
        ExpressionType="SQL",
        InputSerialization={"Parquet": {}},
        OutputSerialization={"JSON": {"RecordDelimiter": "\n"}},
    )

    results = []
    for event in response["Payload"]:
        if "Records" in event:
            results.append(event["Records"]["Payload"].decode("utf-8"))

    return "".join(results)


# Example queries
EXAMPLE_QUERIES = {
    # Basic selection
    "all_columns": "SELECT * FROM s3object s",
    "specific_columns": "SELECT s.name, s.age FROM s3object s",

    # Filtering
    "filter_by_age": "SELECT * FROM s3object s WHERE s.age > 25",
    "filter_by_city": "SELECT * FROM s3object s WHERE s.city = 'NYC'",
    "filter_combined": "SELECT * FROM s3object s WHERE s.age > 25 AND s.city = 'NYC'",

    # Aggregations
    "count_records": "SELECT COUNT(*) FROM s3object s",
    "average_salary": "SELECT AVG(CAST(s.salary AS DECIMAL)) FROM s3object s",
    "sum_by_city": "SELECT s.city, SUM(CAST(s.salary AS DECIMAL)) FROM s3object s GROUP BY s.city",

    # String functions
    "uppercase_names": "SELECT UPPER(s.name) AS name_upper FROM s3object s",
    "filter_like": "SELECT * FROM s3object s WHERE s.name LIKE 'J%'",

    # Limiting results
    "first_10": "SELECT * FROM s3object s LIMIT 10",
}


if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description="NebulaIO S3 Select Example")
    parser.add_argument("bucket", help="S3 bucket name")
    parser.add_argument("key", help="Object key (file path)")
    parser.add_argument("--query", "-q", default="SELECT * FROM s3object s LIMIT 5",
                        help="SQL query to execute")
    parser.add_argument("--format", "-f", choices=["csv", "json", "parquet"],
                        default="csv", help="Input file format")

    args = parser.parse_args()

    print(f"Bucket: {args.bucket}")
    print(f"Key: {args.key}")
    print(f"Query: {args.query}")
    print(f"Format: {args.format}")
    print("-" * 50)

    if args.format == "csv":
        result = select_from_csv(args.bucket, args.key, args.query)
    elif args.format == "json":
        result = select_from_json(args.bucket, args.key, args.query)
    else:
        result = select_from_parquet(args.bucket, args.key, args.query)

    print("Results:")
    print(result)
