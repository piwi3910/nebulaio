#!/usr/bin/env python3
"""
NebulaIO Audit Log Export Example

Demonstrates querying and exporting audit logs for compliance
and security analysis.
"""

import os
import json
import requests
from datetime import datetime, timedelta
from typing import Optional, List, Dict, Any

# Configuration
ADMIN_ENDPOINT = os.getenv("NEBULAIO_ADMIN_ENDPOINT", "http://localhost:9001")
ADMIN_TOKEN = os.getenv("NEBULAIO_ADMIN_TOKEN", "")


class AuditLogClient:
    """Client for NebulaIO Audit Log API."""

    def __init__(self, endpoint: str, token: str):
        self.endpoint = endpoint.rstrip("/")
        self.session = requests.Session()
        self.session.headers.update({
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
        })

    def query(
        self,
        start_time: str = None,
        end_time: str = None,
        event_types: List[str] = None,
        user_id: str = None,
        bucket: str = None,
        object_key: str = None,
        source_ip: str = None,
        success_only: bool = None,
        limit: int = 100,
        offset: int = 0,
    ) -> Dict[str, Any]:
        """Query audit log events."""
        params = {
            "limit": limit,
            "offset": offset,
        }

        if start_time:
            params["start_time"] = start_time
        if end_time:
            params["end_time"] = end_time
        if event_types:
            params["event_types"] = ",".join(event_types)
        if user_id:
            params["user_id"] = user_id
        if bucket:
            params["bucket"] = bucket
        if object_key:
            params["object_key"] = object_key
        if source_ip:
            params["source_ip"] = source_ip
        if success_only is not None:
            params["success_only"] = str(success_only).lower()

        response = self.session.get(
            f"{self.endpoint}/admin/audit/events",
            params=params
        )
        response.raise_for_status()
        return response.json()

    def get_event(self, event_id: str) -> Dict[str, Any]:
        """Get a specific audit event by ID."""
        response = self.session.get(
            f"{self.endpoint}/admin/audit/events/{event_id}"
        )
        response.raise_for_status()
        return response.json()

    def verify_integrity(self, start_id: str = None, end_id: str = None) -> Dict[str, Any]:
        """Verify the integrity chain of audit logs."""
        params = {}
        if start_id:
            params["start_id"] = start_id
        if end_id:
            params["end_id"] = end_id

        response = self.session.post(
            f"{self.endpoint}/admin/audit/verify",
            params=params
        )
        response.raise_for_status()
        return response.json()

    def export(
        self,
        format: str = "json",
        start_time: str = None,
        end_time: str = None,
        event_types: List[str] = None,
    ) -> bytes:
        """Export audit logs in specified format."""
        params = {"format": format}

        if start_time:
            params["start_time"] = start_time
        if end_time:
            params["end_time"] = end_time
        if event_types:
            params["event_types"] = ",".join(event_types)

        response = self.session.get(
            f"{self.endpoint}/admin/audit/export",
            params=params
        )
        response.raise_for_status()
        return response.content

    def get_stats(
        self,
        start_time: str = None,
        end_time: str = None,
    ) -> Dict[str, Any]:
        """Get audit log statistics."""
        params = {}
        if start_time:
            params["start_time"] = start_time
        if end_time:
            params["end_time"] = end_time

        response = self.session.get(
            f"{self.endpoint}/admin/audit/stats",
            params=params
        )
        response.raise_for_status()
        return response.json()


def format_event(event: Dict[str, Any]) -> str:
    """Format an audit event for display."""
    return (
        f"[{event['timestamp']}] "
        f"{event['event_type']:<25} "
        f"user={event.get('user_id', 'anonymous'):<15} "
        f"bucket={event.get('bucket', '-'):<15} "
        f"key={event.get('object_key', '-')[:30]:<30} "
        f"{'SUCCESS' if event.get('success') else 'FAILED'}"
    )


def example_security_audit():
    """Example: Security audit for failed access attempts."""
    client = AuditLogClient(ADMIN_ENDPOINT, ADMIN_TOKEN)

    # Query last 24 hours of failed authentication attempts
    yesterday = (datetime.utcnow() - timedelta(days=1)).isoformat() + "Z"

    print("=== Failed Access Attempts (Last 24 Hours) ===\n")

    events = client.query(
        start_time=yesterday,
        event_types=["GetObject", "PutObject", "DeleteObject", "ListBucket"],
        success_only=False,
        limit=100,
    )

    failed_events = [e for e in events.get("events", []) if not e.get("success")]

    if not failed_events:
        print("No failed access attempts found.")
        return

    # Group by source IP
    by_ip = {}
    for event in failed_events:
        ip = event.get("source_ip", "unknown")
        if ip not in by_ip:
            by_ip[ip] = []
        by_ip[ip].append(event)

    print(f"Found {len(failed_events)} failed attempts from {len(by_ip)} unique IPs\n")

    for ip, events in sorted(by_ip.items(), key=lambda x: -len(x[1])):
        print(f"\nSource IP: {ip} ({len(events)} attempts)")
        print("-" * 60)
        for event in events[:5]:  # Show first 5
            print(f"  {format_event(event)}")
        if len(events) > 5:
            print(f"  ... and {len(events) - 5} more")


def example_compliance_report():
    """Example: Generate compliance report for data access."""
    client = AuditLogClient(ADMIN_ENDPOINT, ADMIN_TOKEN)

    # Get last 7 days
    week_ago = (datetime.utcnow() - timedelta(days=7)).isoformat() + "Z"

    print("=== Data Access Compliance Report ===\n")
    print(f"Period: {week_ago} to now\n")

    # Get statistics
    stats = client.get_stats(start_time=week_ago)

    print("Summary Statistics:")
    print(f"  Total Events: {stats.get('total_events', 0):,}")
    print(f"  Successful: {stats.get('successful_events', 0):,}")
    print(f"  Failed: {stats.get('failed_events', 0):,}")
    print()

    # Break down by event type
    print("Events by Type:")
    for event_type, count in stats.get("by_event_type", {}).items():
        print(f"  {event_type}: {count:,}")
    print()

    # Top users
    print("Top Users by Activity:")
    for user, count in list(stats.get("by_user", {}).items())[:10]:
        print(f"  {user}: {count:,} events")
    print()

    # Verify integrity
    print("Integrity Verification:")
    integrity = client.verify_integrity()
    if integrity.get("valid"):
        print("  Status: PASSED")
        print(f"  Events Verified: {integrity.get('events_verified', 0):,}")
        print(f"  Chain Length: {integrity.get('chain_length', 0):,}")
    else:
        print("  Status: FAILED")
        print(f"  Error: {integrity.get('error', 'Unknown')}")
        print(f"  First Invalid Event: {integrity.get('first_invalid_id', 'N/A')}")


def example_data_access_history():
    """Example: Track access history for specific object."""
    client = AuditLogClient(ADMIN_ENDPOINT, ADMIN_TOKEN)

    bucket = "sensitive-data"
    key = "customer-records/2024/data.csv"

    print(f"=== Access History: s3://{bucket}/{key} ===\n")

    events = client.query(
        bucket=bucket,
        object_key=key,
        limit=50,
    )

    if not events.get("events"):
        print("No access events found.")
        return

    for event in events["events"]:
        print(format_event(event))
        print(f"    Details: {json.dumps(event.get('details', {}), indent=4)}")
        print()


def example_export_for_siem():
    """Example: Export logs for SIEM integration."""
    client = AuditLogClient(ADMIN_ENDPOINT, ADMIN_TOKEN)

    # Export last 24 hours in SIEM-compatible format
    yesterday = (datetime.utcnow() - timedelta(days=1)).isoformat() + "Z"

    print("=== Exporting Audit Logs for SIEM ===\n")

    # Export as JSON
    json_data = client.export(
        format="json",
        start_time=yesterday,
    )

    output_file = f"audit-export-{datetime.now().strftime('%Y%m%d')}.json"
    with open(output_file, "wb") as f:
        f.write(json_data)

    print(f"Exported {len(json_data):,} bytes to {output_file}")

    # Parse and show summary
    events = json.loads(json_data)
    print(f"Total events: {len(events)}")


def main():
    import argparse

    parser = argparse.ArgumentParser(description="NebulaIO Audit Log Export Example")
    subparsers = parser.add_subparsers(dest="command", help="Command to run")

    # Security audit
    subparsers.add_parser("security", help="Security audit for failed access")

    # Compliance report
    subparsers.add_parser("compliance", help="Generate compliance report")

    # Data access history
    history_parser = subparsers.add_parser("history", help="Object access history")
    history_parser.add_argument("bucket", help="Bucket name")
    history_parser.add_argument("key", help="Object key")

    # Query
    query_parser = subparsers.add_parser("query", help="Query audit logs")
    query_parser.add_argument("--user", help="Filter by user")
    query_parser.add_argument("--bucket", help="Filter by bucket")
    query_parser.add_argument("--type", help="Filter by event type")
    query_parser.add_argument("--days", type=int, default=1, help="Days to look back")
    query_parser.add_argument("--limit", type=int, default=20, help="Max results")

    # Export
    export_parser = subparsers.add_parser("export", help="Export audit logs")
    export_parser.add_argument("--format", choices=["json", "csv", "cef"], default="json")
    export_parser.add_argument("--days", type=int, default=1, help="Days to export")
    export_parser.add_argument("--output", "-o", help="Output file")

    # Verify
    subparsers.add_parser("verify", help="Verify integrity chain")

    args = parser.parse_args()

    client = AuditLogClient(ADMIN_ENDPOINT, ADMIN_TOKEN)

    if args.command == "security":
        example_security_audit()

    elif args.command == "compliance":
        example_compliance_report()

    elif args.command == "history":
        events = client.query(bucket=args.bucket, object_key=args.key, limit=50)
        print(f"=== Access History: s3://{args.bucket}/{args.key} ===\n")
        for event in events.get("events", []):
            print(format_event(event))

    elif args.command == "query":
        start_time = (datetime.utcnow() - timedelta(days=args.days)).isoformat() + "Z"
        event_types = [args.type] if args.type else None

        events = client.query(
            start_time=start_time,
            user_id=args.user,
            bucket=args.bucket,
            event_types=event_types,
            limit=args.limit,
        )

        for event in events.get("events", []):
            print(format_event(event))

    elif args.command == "export":
        start_time = (datetime.utcnow() - timedelta(days=args.days)).isoformat() + "Z"
        data = client.export(format=args.format, start_time=start_time)

        if args.output:
            with open(args.output, "wb") as f:
                f.write(data)
            print(f"Exported to {args.output}")
        else:
            print(data.decode("utf-8"))

    elif args.command == "verify":
        result = client.verify_integrity()
        if result.get("valid"):
            print("Integrity verification: PASSED")
            print(f"Events verified: {result.get('events_verified', 0)}")
        else:
            print("Integrity verification: FAILED")
            print(f"Error: {result.get('error', 'Unknown')}")

    else:
        parser.print_help()


if __name__ == "__main__":
    main()
