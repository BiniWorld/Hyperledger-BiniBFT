output "instance_ipv4_public_ip_addr" {
    value = aws_instance.bft_instance.public_ip
}

output "instance_ipv4_private_ip_addr" {
  value = aws_instance.bft_instance.private_ip
}