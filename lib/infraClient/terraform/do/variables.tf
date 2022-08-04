variable "cluster-id" {
  default = "kl"
}

variable "do-token" {
  # default = "dop_v1_9f19ced8f99f078b2f8bc894b18317424400a253b0eefa3050971065c03c125e"
}

variable "size" {
  default = "s-4vcpu-8gb"
}

variable "region" {
  default = "blr1"
}

variable "join-token" {
  default = "node_token_v1_sldkfjslkdjfskljioweuroiqlksdfjowersjdkjfnsdfkjoqwoeiruls"
}

variable "keys-path" {
}

variable "do-image-id" {
  default = "ubuntu-20-04-x64"
  # default = "105910703"
}

variable "ssh_keys" {
  default = ["25:d8:56:2b:70:15:43:c5:dd:e2:ff:d7:47:1b:68:22"]
}
