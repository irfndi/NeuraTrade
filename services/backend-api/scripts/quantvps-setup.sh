#!/bin/bash
# QuantVPS Server Setup Script - Run on the VPS after deployment
set -euo pipefail

echo "[INFO] Setting up NeuraTrade on QuantVPS..."

# Update system
apt-get update
apt-get install -y docker.io docker-compose git curl wget

# Enable and start Docker
systemctl enable docker
systemctl start docker

# Create neuratrade user
useradd -m -s /bin/bash neuratrade || true
usermod -aG docker neuratrade

# Setup directory structure
mkdir -p /opt/neuratrade/{data,logs,backups}
chown -R neuratrade:neuratrade /opt/neuratrade

# Install monitoring
wget -q https://github.com/prometheus/node_exporter/releases/download/v1.6.1/node_exporter-1.6.1.linux-amd64.tar.gz
tar xzf node_exporter-1.6.1.linux-amd64.tar.gz
mv node_exporter-1.6.1.linux-amd64/node_exporter /usr/local/bin/
rm -rf node_exporter-1.6.1.linux-amd64*

cat > /etc/systemd/system/node_exporter.service << 'EOF'
[Unit]
Description=Node Exporter
After=network.target
[Service]
Type=simple
ExecStart=/usr/local/bin/node_exporter
Restart=always
[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable node_exporter
systemctl start node_exporter

# Setup log rotation
cat > /etc/logrotate.d/neuratrade << 'EOF'
/opt/neuratrade/logs/*.log {
    daily
    rotate 7
    compress
    delaycompress
    missingok
    notifempty
    create 0644 neuratrade neuratrade
}
EOF

# Configure firewall
ufw default deny incoming
ufw default allow outgoing
ufw allow ssh
ufw allow 8080/tcp  # API port
ufw allow 9090/tcp  # Prometheus
ufw --force enable

echo "[INFO] QuantVPS setup complete"
echo "[INFO] Start services with: cd /opt/neuratrade && docker-compose up -d"
