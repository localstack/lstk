terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
  }
  backend "s3" {
    bucket = "lstk-tf-remote-state"
    key    = "consumer/terraform.tfstate"
    region = "us-east-1"
  }
}

provider "aws" {
  region = "us-east-1"
}

data "terraform_remote_state" "producer" {
  backend = "s3"
  config = {
    bucket = "lstk-tf-remote-state"
    key    = "producer/terraform.tfstate"
    region = "us-east-1"
  }
}

output "producer_bucket" {
  value = data.terraform_remote_state.producer.outputs.bucket_name
}
