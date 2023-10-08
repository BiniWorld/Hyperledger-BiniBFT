variable "region" {
  description = "Default region for provider"
  type        = string
  default     = "ap-south-1"
}

variable "ami" {
  description = "Amazon machine image to use for ec2 instance"
  type        = string
  default     = "ami-0f5ee92e2d63afc18" # Ubuntu 20.04 LTS // ap-south-1
}

variable "instance_type" {
  description = "ec2 instance type"
  type        = string
  default     = "t2.micro"
}


variable "instance_name" {
  description = "Name of ec2 instance"
  type        = string
}