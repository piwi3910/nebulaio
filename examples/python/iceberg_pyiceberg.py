#!/usr/bin/env python3
"""
Apache Iceberg with PyIceberg Example

Demonstrates direct Iceberg table access using PyIceberg library.
"""

import os
from datetime import datetime
from pyiceberg.catalog import load_catalog
from pyiceberg.schema import Schema
from pyiceberg.types import (
    NestedField,
    LongType,
    StringType,
    TimestampType,
    DoubleType,
)
from pyiceberg.partitioning import PartitionSpec, PartitionField
from pyiceberg.transforms import DayTransform

# Configuration
ICEBERG_ENDPOINT = os.getenv("NEBULAIO_ICEBERG_ENDPOINT", "http://localhost:9006")
S3_ENDPOINT = os.getenv("NEBULAIO_ENDPOINT", "http://localhost:9000")
ACCESS_KEY = os.getenv("NEBULAIO_ACCESS_KEY", "minioadmin")
SECRET_KEY = os.getenv("NEBULAIO_SECRET_KEY", "minioadmin")


def create_catalog():
    """Create PyIceberg catalog connected to NebulaIO."""
    return load_catalog(
        "nebulaio",
        **{
            "type": "rest",
            "uri": f"{ICEBERG_ENDPOINT}/iceberg",
            "warehouse": "s3://warehouse/",
            "s3.endpoint": S3_ENDPOINT,
            "s3.access-key-id": ACCESS_KEY,
            "s3.secret-access-key": SECRET_KEY,
        }
    )


def create_namespace(catalog, namespace: str):
    """Create a namespace if it doesn't exist."""
    try:
        catalog.create_namespace(namespace)
        print(f"Created namespace: {namespace}")
    except Exception as e:
        if "already exists" in str(e).lower():
            print(f"Namespace already exists: {namespace}")
        else:
            raise


def create_table(catalog, table_name: str):
    """Create an Iceberg table with schema and partitioning."""
    schema = Schema(
        NestedField(1, "id", LongType(), required=True),
        NestedField(2, "event_type", StringType(), required=True),
        NestedField(3, "user_id", StringType()),
        NestedField(4, "timestamp", TimestampType()),
        NestedField(5, "value", DoubleType()),
        NestedField(6, "metadata", StringType()),
    )

    partition_spec = PartitionSpec(
        PartitionField(
            source_id=4,
            field_id=1000,
            transform=DayTransform(),
            name="day",
        )
    )

    try:
        table = catalog.create_table(
            table_name,
            schema=schema,
            partition_spec=partition_spec,
        )
        print(f"Created table: {table_name}")
        return table
    except Exception as e:
        if "already exists" in str(e).lower():
            print(f"Table already exists: {table_name}")
            return catalog.load_table(table_name)
        raise


def insert_data(table, records: list[dict]):
    """Insert records into the table."""
    import pyarrow as pa

    # Convert to PyArrow table
    arrow_table = pa.Table.from_pylist(records)

    # Append to Iceberg table
    table.append(arrow_table)
    print(f"Inserted {len(records)} records")


def query_table(table, filter_expr: str = None):
    """Query table data."""
    scan = table.scan()

    if filter_expr:
        # Note: PyIceberg uses row_filter for filtering
        scan = scan.filter(filter_expr)

    # Convert to pandas
    df = scan.to_pandas()
    return df


def time_travel(table, snapshot_id: int = None, as_of: datetime = None):
    """Query historical data using time travel."""
    if snapshot_id:
        scan = table.scan(snapshot_id=snapshot_id)
    elif as_of:
        # Find snapshot as of timestamp
        for snapshot in table.snapshots():
            if snapshot.timestamp_ms <= as_of.timestamp() * 1000:
                scan = table.scan(snapshot_id=snapshot.snapshot_id)
                break
        else:
            raise ValueError(f"No snapshot found before {as_of}")
    else:
        scan = table.scan()

    return scan.to_pandas()


def list_snapshots(table):
    """List all snapshots (for time travel)."""
    print("\nTable Snapshots:")
    for snapshot in table.snapshots():
        ts = datetime.fromtimestamp(snapshot.timestamp_ms / 1000)
        print(f"  ID: {snapshot.snapshot_id}")
        print(f"  Time: {ts}")
        print(f"  Summary: {snapshot.summary}")
        print()


def schema_evolution(table):
    """Demonstrate schema evolution."""
    print("\nSchema Evolution:")

    # Add a new column
    with table.update_schema() as update:
        update.add_column("category", StringType())
    print("  Added column: category")

    # Rename a column
    with table.update_schema() as update:
        update.rename_column("metadata", "extra_data")
    print("  Renamed column: metadata -> extra_data")


def main():
    print("Apache Iceberg with PyIceberg Example")
    print("=" * 50)

    # Create catalog
    print("\n1. Connecting to catalog...")
    catalog = create_catalog()
    print(f"   Connected to: {ICEBERG_ENDPOINT}")

    # Create namespace
    print("\n2. Creating namespace...")
    create_namespace(catalog, "analytics")

    # Create table
    print("\n3. Creating table...")
    table = create_table(catalog, "analytics.events")

    # Show schema
    print("\n4. Table schema:")
    for field in table.schema().fields:
        print(f"   {field.field_id}: {field.name} ({field.field_type})")

    # Insert data
    print("\n5. Inserting data...")
    records = [
        {
            "id": i,
            "event_type": ["click", "view", "purchase"][i % 3],
            "user_id": f"user_{i % 100}",
            "timestamp": datetime.now(),
            "value": float(i * 1.5),
            "metadata": f'{{"source": "web", "version": {i}}}',
        }
        for i in range(1000)
    ]
    insert_data(table, records)

    # Query data
    print("\n6. Querying data...")
    df = query_table(table)
    print(f"   Total records: {len(df)}")
    print(f"   Sample:\n{df.head()}")

    # Show snapshots
    list_snapshots(table)

    # Insert more data (creates new snapshot)
    print("\n7. Inserting more data (new snapshot)...")
    more_records = [
        {
            "id": 1000 + i,
            "event_type": "signup",
            "user_id": f"user_{1000 + i}",
            "timestamp": datetime.now(),
            "value": 0.0,
            "metadata": '{"source": "mobile"}',
        }
        for i in range(100)
    ]
    insert_data(table, more_records)

    # Query with time travel
    print("\n8. Time travel query (previous snapshot):")
    snapshots = list(table.snapshots())
    if len(snapshots) >= 2:
        old_snapshot = snapshots[-2]
        old_df = time_travel(table, snapshot_id=old_snapshot.snapshot_id)
        print(f"   Records in previous snapshot: {len(old_df)}")

    # Current count
    current_df = query_table(table)
    print(f"   Records in current snapshot: {len(current_df)}")

    print("\nDone!")


if __name__ == "__main__":
    main()
