#!/bin/bash

# Fix Docker repository for Debian
echo "deb [arch=amd64] https://download.docker.com/linux/debian trixie stable" | sudo tee /etc/apt/sources.list.d/docker.list

# Update and install Docker
sudo apt-get update
sudo apt-get install -y docker.io docker-compose

# Start Docker
sudo systemctl start docker
sudo systemctl enable docker

# Run Online Trail
docker run -d -p 8080:8080 --restart unless-stopped --name online-trail xphox2/online-trail

echo "Online Trail is now running on port 8080!"
echo "Open http://your-server-ip:8080 in your browser"
