implement way to reset hubfly-builder.sqlite
LATEST=$(curl -s https://api.github.com/repos/moby/buildkit/releases/latest | grep browser_download_url | grep linux |grep amd64 | cut -d '"' -f 4)

wget $LATEST -O buildkit.tgz
tar -xvf buildkit.tgz
sudo mv bin/buildctl /usr/local/bin/
sudo mv bin/buildkitd /usr/local/bin/
sudo chmod +x /usr/local/bin/buildctl /usr/local/bin/buildkitd


curl -s http://localhost:5000/v2/_catalog | jq -r '.repositories[]'
