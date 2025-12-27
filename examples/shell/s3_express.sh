#!/bin/bash
#
# S3 Express One Zone Shell Examples
#
# Demonstrates using S3 Express features via CLI

set -e

# Configuration
ENDPOINT="${NEBULAIO_ENDPOINT:-http://localhost:9000}"
ACCESS_KEY="${NEBULAIO_ACCESS_KEY:-minioadmin}"
SECRET_KEY="${NEBULAIO_SECRET_KEY:-minioadmin}"
ZONE="${S3_EXPRESS_ZONE:-use1-az1}"

# AWS CLI with NebulaIO endpoint
alias s3="aws --endpoint-url $ENDPOINT s3"
alias s3api="aws --endpoint-url $ENDPOINT s3api"

echo "S3 Express One Zone Examples"
echo "============================"

# Create Express bucket (directory bucket)
echo ""
echo "1. Creating Express bucket..."
BUCKET="express-demo--${ZONE}--x-s3"

aws --endpoint-url "$ENDPOINT" s3api create-bucket \
    --bucket "$BUCKET" \
    --create-bucket-configuration '{
        "LocationConstraint": "'$ZONE'",
        "Location": {"Type": "AvailabilityZone", "Name": "'$ZONE'"},
        "Bucket": {"DataRedundancy": "SingleAvailabilityZone", "Type": "Directory"}
    }' 2>/dev/null || echo "   Bucket already exists"

echo "   Created: $BUCKET"

# Upload with low latency
echo ""
echo "2. Low-latency upload..."
echo "Hello, S3 Express!" > /tmp/test.txt

START=$(date +%s%N)
aws --endpoint-url "$ENDPOINT" s3 cp /tmp/test.txt "s3://$BUCKET/data/test.txt"
END=$(date +%s%N)
ELAPSED=$(( (END - START) / 1000000 ))
echo "   Upload completed in ${ELAPSED}ms"

# Download with low latency
echo ""
echo "3. Low-latency download..."
START=$(date +%s%N)
aws --endpoint-url "$ENDPOINT" s3 cp "s3://$BUCKET/data/test.txt" /tmp/downloaded.txt
END=$(date +%s%N)
ELAPSED=$(( (END - START) / 1000000 ))
echo "   Download completed in ${ELAPSED}ms"
cat /tmp/downloaded.txt

# Atomic append (using custom endpoint)
echo ""
echo "4. Atomic append (streaming log)..."
for i in {1..5}; do
    curl -s -X POST "$ENDPOINT/$BUCKET/logs/app.log?append" \
        -H "Content-Type: text/plain" \
        -u "$ACCESS_KEY:$SECRET_KEY" \
        -d "Log entry $i at $(date)"
    echo "   Appended entry $i"
done

# Read appended content
echo ""
echo "5. Reading appended log..."
aws --endpoint-url "$ENDPOINT" s3 cp "s3://$BUCKET/logs/app.log" -

# List objects with prefix
echo ""
echo "6. Listing objects..."
aws --endpoint-url "$ENDPOINT" s3 ls "s3://$BUCKET/" --recursive

# Performance benchmark
echo ""
echo "7. Performance benchmark (10 operations)..."
echo "   Testing PUT latency..."

TOTAL=0
for i in {1..10}; do
    echo "test data $i" > /tmp/bench.txt
    START=$(date +%s%N)
    aws --endpoint-url "$ENDPOINT" s3 cp /tmp/bench.txt "s3://$BUCKET/bench/test-$i.txt" --quiet
    END=$(date +%s%N)
    ELAPSED=$(( (END - START) / 1000000 ))
    TOTAL=$((TOTAL + ELAPSED))
done
AVG=$((TOTAL / 10))
echo "   Average PUT latency: ${AVG}ms"

# Create session (for reduced auth overhead)
echo ""
echo "8. Creating session..."
SESSION=$(curl -s -X POST "$ENDPOINT/?session" \
    -H "X-Amz-Create-Session-Mode: ReadWrite" \
    -u "$ACCESS_KEY:$SECRET_KEY")
echo "   Session created"

# Cleanup
echo ""
echo "9. Cleanup..."
rm -f /tmp/test.txt /tmp/downloaded.txt /tmp/bench.txt

echo ""
echo "Done!"
echo ""
echo "Express bucket: $BUCKET"
echo "Endpoint: $ENDPOINT"
