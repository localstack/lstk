terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
  }
  backend "s3" {
    bucket         = "lstk-tf-state-locking"
    key            = "s3-backend-locking/terraform.tfstate"
    region         = "us-east-1"
    dynamodb_table = "lstk-tf-locks"
  }
}

provider "aws" {
  region = "us-east-1"
}

resource "aws_s3_bucket" "b" {
  bucket = "lstk-e2e-s3-backend-locking"
}
