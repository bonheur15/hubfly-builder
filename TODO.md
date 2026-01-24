docker run -d -p 5000:5000 --restart always --name registry registry:3

LATEST=$(curl -s https://api.github.com/repos/moby/buildkit/releases/latest | grep browser_download_url | grep linux |grep amd64 | cut -d '"' -f 4)

wget $LATEST -O buildkit.tgz
tar -xvf buildkit.tgz
sudo mv bin/buildctl /usr/local/bin/
sudo mv bin/buildkitd /usr/local/bin/
sudo chmod +x /usr/local/bin/buildctl /usr/local/bin/buildkitd


curl -s http://100.106.206.92:32768/v2/_catalog | jq -r '.repositories[]'
curl -s http://100.106.206.92:32768/v2/user-123/my-project/tags/list

curl -s -I -H "Accept: application/vnd.docker.distribution.manifest.v2+json" \
  http://100.106.206.92:32768/v2/user-123/my-project/manifests/latest




sudo mkdir -p /etc/docker
sudo nano /etc/docker/daemon.json
{
  "insecure-registries": ["100.106.206.92:32768"]
}
sudo systemctl restart docker
