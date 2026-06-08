terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
  }
}

# A provider that explicitly targets a public (FIPS) S3 endpoint, as some
# customers do. lstk's generated override is expected to win and point S3 at
# LocalStack instead.
provider "aws" {
  region = "us-east-1"
  endpoints {
    s3 = "https://s3-fips.us-east-1.amazonaws.com"
  }
}

resource "aws_s3_bucket" "b" {
  bucket = "lstk-e2e-fips"
}
