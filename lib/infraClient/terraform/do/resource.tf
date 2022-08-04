terraform {
  required_providers {
    digitalocean = {
      source  = "digitalocean/digitalocean"
      version = "2.19.0"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "3.1.0"
    }
  }
}

provider "digitalocean" {
  token = var.do-token
}

resource "digitalocean_droplet" "agent-node"  {
  image    = var.do-image-id
  name     = "${var.cluster-id}-byoc-node-${var.accountId}"
  region = var.region
  size     = var.size
  ssh_keys = var.ssh_keys
  user_data = templatefile("./init.sh", {
    pubkey = file("${var.keys-path}/access.pub")
  })
}

# output "master-nodes-count" {
#   value = var.master-nodes-count
# }

# output "agent-nodes-count" {
#   value = var.agent-nodes-count
# }

# output "master-ips" {
#   value = join(",", digitalocean_droplet.master-nodes.*.ipv4_address)
# }

output "agent-ip" {
  value =  digitalocean_droplet.agent-node.ipv4_address
}

# output "master-internal-ip" {
#   value = digitalocean_droplet.master-nodes[0].ipv4_address
# }
