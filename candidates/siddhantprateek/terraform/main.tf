terraform {
    required_providers {
      aws = {
        source = "hashicorp/aws",
        version = "~> 3.0"
      }
    }
}

provider "aws" {
    region = var.region
}

locals {
  extra_tag = "bft_tag"
}

resource "aws_instance" "bft_instance" {
  ami = var.ami
  instance_type = var.instance_type

  tags = {
    Name = var.instance_name
    ExtraTag = local.extra_tag
  }
}