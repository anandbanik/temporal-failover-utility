# AWS Lambda Deployment

This service supports two run modes. When the `AWS_LAMBDA_RUNTIME_API` environment variable is present (set automatically by the Lambda runtime), it starts as a Lambda handler. Otherwise it runs as a local HTTP server on `SERVER_PORT` (default `9090`).

## Build

```bash
# Local binary (unchanged)
make build

# Lambda binary + zip artifact
make zip
```

`make zip` produces `bootstrap.zip` containing the `bootstrap` binary compiled for `linux/arm64`.

## AWS Setup (one-time)

### 1. IAM execution role

```bash
aws iam create-role \
  --role-name temporal-utility-lambda-role \
  --assume-role-policy-document '{
    "Version":"2012-10-17",
    "Statement":[{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:AssumeRole"}]
  }'

aws iam attach-role-policy \
  --role-name temporal-utility-lambda-role \
  --policy-arn arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole
```

If Temporal runs inside a VPC, also attach `AWSLambdaVPCAccessExecutionRole` and configure the function's VPC settings (subnet + security group with outbound TCP:7233 to Temporal).

### 2. Create the Lambda function

```bash
make zip

aws lambda create-function \
  --function-name temporal-failover-utility \
  --runtime provided.al2023 \
  --architectures arm64 \
  --handler bootstrap \
  --zip-file fileb://bootstrap.zip \
  --role arn:aws:iam::<ACCOUNT_ID>:role/temporal-utility-lambda-role \
  --timeout 30 \
  --memory-size 256 \
  --environment Variables="{
    TEMPORAL_HOST_PORT=<temporal-host:7233>,
    GIN_MODE=release,
    OTEL_SERVICE_NAME=temporal-utility,
    OTEL_SERVICE_VERSION=1.0.0
  }"
```

### 3. Update after code changes

```bash
make zip
aws lambda update-function-code \
  --function-name temporal-failover-utility \
  --zip-file fileb://bootstrap.zip
```

## API Gateway

Create a REST API with Lambda Proxy integration via the AWS Console:

1. **API Gateway** → Create API → REST API → New API → name `temporal-failover-utility`
2. Create resource `/{proxy+}` with **"Configure as proxy resource"** checked
3. Integration: **Lambda Function Proxy** → `temporal-failover-utility`
4. Deploy to stage `prod`

Grant API Gateway permission to invoke the function:

```bash
aws lambda add-permission \
  --function-name temporal-failover-utility \
  --statement-id apigateway-invoke \
  --action lambda:InvokeFunction \
  --principal apigateway.amazonaws.com \
  --source-arn "arn:aws:execute-api:<REGION>:<ACCOUNT_ID>:<API_ID>/*/*"
```

## Lambda Configuration Reference

| Parameter | Value |
|---|---|
| Runtime | `provided.al2023` |
| Handler | `bootstrap` |
| Architecture | `arm64` |
| Timeout | 30s |
| Memory | 256 MB |
| `TEMPORAL_HOST_PORT` | Required — e.g. `temporal.internal:7233` |
| `GIN_MODE` | `release` |
| `OTEL_SERVICE_NAME` | `temporal-utility` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Optional — leave empty to fall back to stdout |
| `SERVER_PORT` | Not used in Lambda mode |

## Verification

```bash
BASE=https://<API_ID>.execute-api.<REGION>.amazonaws.com/prod

curl -s $BASE/healthz
# → {"status":"ok"}

curl -s -X POST $BASE/api/v1/namespaces \
  -H "Content-Type: application/json" \
  -d '{"name":"test-ns","retention_days":7}'

curl -s -X POST $BASE/api/v1/clusters \
  -H "Content-Type: application/json" \
  -d '{"frontend_address":"remote:7233","enable_connection":true}'
```

CloudWatch logs: `/aws/lambda/temporal-failover-utility`

## Notes

- **Temporal in VPC**: Lambda must be in the same VPC with outbound TCP:7233 allowed to Temporal's security group.
- **Warm reuse**: The Temporal gRPC connection and OTel SDK are initialized once per cold start and reused across warm invocations.
- **OTEL trace flushing**: The 5s batch exporter may lose spans when the execution environment is frozen. Acceptable for an ops tool; use a Lambda Extension sidecar if strict trace completeness is required.
- **Swagger UI**: The `@host localhost:9090` annotation means the "Try it out" button targets localhost. Update the annotation to the API Gateway domain and re-run `make swag` to fix this.
- **NAT Gateway**: Required if `OTEL_EXPORTER_OTLP_ENDPOINT` points to an internet-accessible collector and Lambda runs in a private subnet.
