terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
  }
  backend "s3" {
    bucket = "lstk-tf-remote-state"
    key    = "producer/terraform.tfstate"
    region = "us-east-1"
  }
}

provider "aws" {
  region = "us-east-1"
}

resource "aws_s3_bucket" "b" {
  bucket = "lstk-e2e-remote-producer"
}

output "bucket_name" {
  value = aws_s3_bucket.b.bucket
}
