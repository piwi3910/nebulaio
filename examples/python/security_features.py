#!/usr/bin/env python3
"""
NebulaIO Security Features Examples

This script demonstrates the security features of NebulaIO:
- Access Analytics and Anomaly Detection
- Encryption Key Rotation
- mTLS Certificate Management
- Distributed Tracing
"""

import requests
import json
from datetime import datetime, timedelta
from typing import Optional, Dict, Any, List


class NebulaIOSecurityClient:
    """Client for NebulaIO security features."""

    def __init__(
        self,
        admin_endpoint: str = "http://localhost:9001",
        token: Optional[str] = None
    ):
        self.admin_endpoint = admin_endpoint.rstrip("/")
        self.session = requests.Session()
        if token:
            self.session.headers["Authorization"] = f"Bearer {token}"

    # =========================================================================
    # Access Analytics
    # =========================================================================

    def get_anomalies(
        self,
        severity: Optional[str] = None,
        anomaly_type: Optional[str] = None,
        user_id: Optional[str] = None,
        acknowledged: Optional[bool] = None,
        start_time: Optional[datetime] = None,
        end_time: Optional[datetime] = None,
        limit: int = 100
    ) -> List[Dict[str, Any]]:
        """Get detected anomalies."""
        params = {"limit": limit}
        if severity:
            params["severity"] = severity
        if anomaly_type:
            params["type"] = anomaly_type
        if user_id:
            params["user_id"] = user_id
        if acknowledged is not None:
            params["acknowledged"] = str(acknowledged).lower()
        if start_time:
            params["start_time"] = start_time.isoformat() + "Z"
        if end_time:
            params["end_time"] = end_time.isoformat() + "Z"

        response = self.session.get(
            f"{self.admin_endpoint}/api/v1/analytics/anomalies",
            params=params
        )
        response.raise_for_status()
        return response.json()["anomalies"]

    def acknowledge_anomaly(
        self,
        anomaly_id: str,
        resolution: str
    ) -> Dict[str, Any]:
        """Acknowledge an anomaly."""
        response = self.session.post(
            f"{self.admin_endpoint}/api/v1/analytics/anomalies/{anomaly_id}/acknowledge",
            json={"resolution": resolution}
        )
        response.raise_for_status()
        return response.json()

    def get_user_baseline(self, user_id: str) -> Dict[str, Any]:
        """Get behavior baseline for a user."""
        response = self.session.get(
            f"{self.admin_endpoint}/api/v1/analytics/baselines/{user_id}"
        )
        response.raise_for_status()
        return response.json()

    def generate_analytics_report(
        self,
        start_time: datetime,
        end_time: datetime,
        format: str = "json"
    ) -> Dict[str, Any]:
        """Generate an analytics report."""
        response = self.session.post(
            f"{self.admin_endpoint}/api/v1/analytics/reports",
            json={
                "start_time": start_time.isoformat() + "Z",
                "end_time": end_time.isoformat() + "Z",
                "format": format
            }
        )
        response.raise_for_status()
        return response.json()

    def add_custom_rule(self, rule: Dict[str, Any]) -> Dict[str, Any]:
        """Add a custom anomaly detection rule."""
        response = self.session.post(
            f"{self.admin_endpoint}/api/v1/analytics/rules",
            json=rule
        )
        response.raise_for_status()
        return response.json()

    def get_analytics_stats(self) -> Dict[str, Any]:
        """Get analytics statistics."""
        response = self.session.get(
            f"{self.admin_endpoint}/api/v1/analytics/stats"
        )
        response.raise_for_status()
        return response.json()

    # =========================================================================
    # Encryption Key Rotation
    # =========================================================================

    def create_key(
        self,
        key_type: str,
        algorithm: str = "AES-256-GCM",
        alias: Optional[str] = None,
        metadata: Optional[Dict[str, str]] = None
    ) -> Dict[str, Any]:
        """Create a new encryption key."""
        payload = {
            "type": key_type,
            "algorithm": algorithm
        }
        if alias:
            payload["alias"] = alias
        if metadata:
            payload["metadata"] = metadata

        response = self.session.post(
            f"{self.admin_endpoint}/api/v1/keys",
            json=payload
        )
        response.raise_for_status()
        return response.json()

    def list_keys(
        self,
        key_type: Optional[str] = None,
        status: Optional[str] = None
    ) -> List[Dict[str, Any]]:
        """List encryption keys."""
        params = {}
        if key_type:
            params["type"] = key_type
        if status:
            params["status"] = status

        response = self.session.get(
            f"{self.admin_endpoint}/api/v1/keys",
            params=params
        )
        response.raise_for_status()
        return response.json()["keys"]

    def get_key(self, key_id: str) -> Dict[str, Any]:
        """Get key details."""
        response = self.session.get(
            f"{self.admin_endpoint}/api/v1/keys/{key_id}"
        )
        response.raise_for_status()
        return response.json()

    def rotate_key(
        self,
        key_id: str,
        trigger: str = "manual"
    ) -> Dict[str, Any]:
        """Rotate an encryption key."""
        response = self.session.post(
            f"{self.admin_endpoint}/api/v1/keys/{key_id}/rotate",
            json={"trigger": trigger}
        )
        response.raise_for_status()
        return response.json()

    def get_rotation_history(self, key_id: str) -> List[Dict[str, Any]]:
        """Get rotation history for a key."""
        response = self.session.get(
            f"{self.admin_endpoint}/api/v1/keys/{key_id}/rotations"
        )
        response.raise_for_status()
        return response.json()["events"]

    def start_reencryption_job(
        self,
        key_id: str,
        old_version: int,
        new_version: int,
        buckets: Optional[List[str]] = None,
        concurrency: int = 10
    ) -> Dict[str, Any]:
        """Start a re-encryption job."""
        payload = {
            "old_version": old_version,
            "new_version": new_version,
            "concurrency": concurrency
        }
        if buckets:
            payload["buckets"] = buckets

        response = self.session.post(
            f"{self.admin_endpoint}/api/v1/keys/{key_id}/reencrypt",
            json=payload
        )
        response.raise_for_status()
        return response.json()

    def get_reencryption_job(self, job_id: str) -> Dict[str, Any]:
        """Get re-encryption job status."""
        response = self.session.get(
            f"{self.admin_endpoint}/api/v1/reencrypt/{job_id}"
        )
        response.raise_for_status()
        return response.json()

    # =========================================================================
    # mTLS Certificate Management
    # =========================================================================

    def init_ca(self, common_name: str) -> Dict[str, Any]:
        """Initialize the Certificate Authority."""
        response = self.session.post(
            f"{self.admin_endpoint}/api/v1/mtls/ca/init",
            json={"common_name": common_name}
        )
        response.raise_for_status()
        return response.json()

    def get_ca_certificate(self) -> str:
        """Get the CA certificate."""
        response = self.session.get(
            f"{self.admin_endpoint}/api/v1/mtls/ca/certificate"
        )
        response.raise_for_status()
        return response.text

    def issue_certificate(
        self,
        cert_type: str,
        common_name: str,
        dns_names: Optional[List[str]] = None,
        ip_addresses: Optional[List[str]] = None
    ) -> Dict[str, Any]:
        """Issue a new certificate."""
        payload = {
            "type": cert_type,
            "common_name": common_name
        }
        if dns_names:
            payload["dns_names"] = dns_names
        if ip_addresses:
            payload["ip_addresses"] = ip_addresses

        response = self.session.post(
            f"{self.admin_endpoint}/api/v1/mtls/certificates",
            json=payload
        )
        response.raise_for_status()
        return response.json()

    def list_certificates(self) -> List[Dict[str, Any]]:
        """List all certificates."""
        response = self.session.get(
            f"{self.admin_endpoint}/api/v1/mtls/certificates"
        )
        response.raise_for_status()
        return response.json()["certificates"]

    def revoke_certificate(
        self,
        cert_id: str,
        reason: str
    ) -> Dict[str, Any]:
        """Revoke a certificate."""
        response = self.session.post(
            f"{self.admin_endpoint}/api/v1/mtls/certificates/{cert_id}/revoke",
            json={"reason": reason}
        )
        response.raise_for_status()
        return response.json()

    def get_crl(self) -> str:
        """Get the Certificate Revocation List."""
        response = self.session.get(
            f"{self.admin_endpoint}/api/v1/mtls/crl"
        )
        response.raise_for_status()
        return response.text

    # =========================================================================
    # Distributed Tracing
    # =========================================================================

    def get_tracing_config(self) -> Dict[str, Any]:
        """Get tracing configuration."""
        response = self.session.get(
            f"{self.admin_endpoint}/api/v1/tracing/config"
        )
        response.raise_for_status()
        return response.json()

    def update_tracing_config(self, config: Dict[str, Any]) -> Dict[str, Any]:
        """Update tracing configuration."""
        response = self.session.put(
            f"{self.admin_endpoint}/api/v1/tracing/config",
            json=config
        )
        response.raise_for_status()
        return response.json()

    def get_tracing_stats(self) -> Dict[str, Any]:
        """Get tracing statistics."""
        response = self.session.get(
            f"{self.admin_endpoint}/api/v1/tracing/stats"
        )
        response.raise_for_status()
        return response.json()


def main():
    """Example usage of security features."""
    client = NebulaIOSecurityClient(token="your-admin-token")

    print("=" * 60)
    print("NebulaIO Security Features Examples")
    print("=" * 60)

    # =========================================================================
    # Access Analytics Examples
    # =========================================================================
    print("\n--- Access Analytics ---\n")

    # Get high severity anomalies
    print("Getting high severity anomalies...")
    try:
        anomalies = client.get_anomalies(
            severity="HIGH",
            start_time=datetime.utcnow() - timedelta(days=7),
            limit=10
        )
        print(f"Found {len(anomalies)} high severity anomalies")
        for anomaly in anomalies[:3]:
            print(f"  - {anomaly['type']}: {anomaly['description']}")
    except Exception as e:
        print(f"  Error: {e}")

    # Get analytics stats
    print("\nGetting analytics stats...")
    try:
        stats = client.get_analytics_stats()
        print(f"  Total events: {stats.get('total_events', 'N/A')}")
        print(f"  Anomalies detected: {stats.get('anomalies_detected', 'N/A')}")
    except Exception as e:
        print(f"  Error: {e}")

    # Add custom rule
    print("\nAdding custom detection rule...")
    try:
        rule = client.add_custom_rule({
            "id": "sensitive-bucket-access",
            "name": "Sensitive Bucket Access",
            "type": "SENSITIVE_ACCESS",
            "enabled": True,
            "severity": "HIGH",
            "threshold": 1,
            "window": "1h",
            "conditions": [
                {"field": "bucket", "operator": "in", "value": ["secrets", "pii"]}
            ]
        })
        print(f"  Created rule: {rule.get('id', 'N/A')}")
    except Exception as e:
        print(f"  Error: {e}")

    # =========================================================================
    # Key Rotation Examples
    # =========================================================================
    print("\n--- Encryption Key Rotation ---\n")

    # Create a new key
    print("Creating encryption key...")
    try:
        key = client.create_key(
            key_type="BUCKET",
            algorithm="AES-256-GCM",
            alias="production-bucket-key",
            metadata={"environment": "production"}
        )
        key_id = key["id"]
        print(f"  Created key: {key_id}")
    except Exception as e:
        print(f"  Error: {e}")
        key_id = None

    # List keys
    print("\nListing data keys...")
    try:
        keys = client.list_keys(key_type="DATA")
        print(f"  Found {len(keys)} data keys")
    except Exception as e:
        print(f"  Error: {e}")

    # Rotate key
    if key_id:
        print(f"\nRotating key {key_id}...")
        try:
            result = client.rotate_key(key_id, trigger="manual")
            print(f"  New version: {result.get('new_version', 'N/A')}")
        except Exception as e:
            print(f"  Error: {e}")

    # =========================================================================
    # mTLS Examples
    # =========================================================================
    print("\n--- mTLS Certificate Management ---\n")

    # Initialize CA (if not already done)
    print("Initializing CA...")
    try:
        ca = client.init_ca("NebulaIO Production CA")
        print(f"  CA initialized: {ca.get('common_name', 'N/A')}")
    except Exception as e:
        print(f"  Error (may already exist): {e}")

    # Issue a server certificate
    print("\nIssuing server certificate...")
    try:
        cert = client.issue_certificate(
            cert_type="SERVER",
            common_name="node-01.cluster.local",
            dns_names=["node-01", "node-01.cluster.local"],
            ip_addresses=["10.0.0.1"]
        )
        print(f"  Issued certificate: {cert.get('id', 'N/A')}")
        print(f"  Expires: {cert.get('not_after', 'N/A')}")
    except Exception as e:
        print(f"  Error: {e}")

    # List certificates
    print("\nListing certificates...")
    try:
        certs = client.list_certificates()
        print(f"  Found {len(certs)} certificates")
        for cert in certs[:3]:
            print(f"    - {cert['common_name']} ({cert['type']})")
    except Exception as e:
        print(f"  Error: {e}")

    # =========================================================================
    # Tracing Examples
    # =========================================================================
    print("\n--- Distributed Tracing ---\n")

    # Get tracing config
    print("Getting tracing configuration...")
    try:
        config = client.get_tracing_config()
        print(f"  Service: {config.get('service_name', 'N/A')}")
        print(f"  Sampling rate: {config.get('sampling', {}).get('rate', 'N/A')}")
        print(f"  Exporter: {config.get('exporter', {}).get('type', 'N/A')}")
    except Exception as e:
        print(f"  Error: {e}")

    # Get tracing stats
    print("\nGetting tracing stats...")
    try:
        stats = client.get_tracing_stats()
        print(f"  Active spans: {stats.get('active_spans', 'N/A')}")
        print(f"  Total spans: {stats.get('total_spans', 'N/A')}")
    except Exception as e:
        print(f"  Error: {e}")

    print("\n" + "=" * 60)
    print("Examples complete!")
    print("=" * 60)


if __name__ == "__main__":
    main()
