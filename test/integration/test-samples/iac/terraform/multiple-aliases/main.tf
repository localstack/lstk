terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
  }
}

provider "aws" {
  region = "us-east-1"
}

provider "aws" {
  alias  = "west"
  region = "us-west-2"
}

resource "aws_s3_bucket" "default" {
  bucket = "lstk-e2e-default"
}

resource "aws_s3_bucket" "west" {
  provider = aws.west
  bucket   = "lstk-e2e-west"
}
