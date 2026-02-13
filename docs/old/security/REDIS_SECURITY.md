# Redis Security Guide for NeuraTrade

## Security Issue Identified

A network security scan identified that Redis was exposed on port 6379 and accessible from the public internet. This could allow unauthorized access to cached data.

## Resolution Steps

### 1. Docker Configuration Changes

**Fixed in docker-compose.yml:**
- Removed port mapping `6379:6379` that exposed Redis to the public internet
- Added secure Redis configuration via `/etc/redis/redis.conf`
- Created Docker-specific Redis configuration that allows internal container communication but blocks external access

### 2. Redis Configuration Files

**Unified Configuration (`redis.conf`):**
- Configured for Docker container environment
- Uses Docker's internal networking for container communication
- Blocks external internet access via Docker network isolation
- Maintains security within containerized environments

### 3. Production Deployment Security

#### DigitalOcean Cloud Firewall Configuration

Create a firewall rule to block external access to Redis port 6379:

```bash
# Create a new firewall (if not already created)
doctl compute firewall create \
  --name neuratrade-redis-security \
  --droplet-ids YOUR_DROPLET_ID \
  --inbound-rules "protocol:tcp,ports:22,address:YOUR_IP/32" \
  --inbound-rules "protocol:tcp,ports:80,address:0.0.0.0/0,::/0" \
  --inbound-rules "protocol:tcp,ports:443,address:0.0.0.0/0,::/0" \
  --outbound-rules "protocol:tcp,ports:1-65535,address:0.0.0.0/0,::/0" \
  --outbound-rules "protocol:udp,ports:53,address:0.0.0.0/0,::/0"

# Note: This firewall explicitly does NOT allow port 6379 inbound
```

#### UFW Configuration (Ubuntu)

If using UFW on the droplet:

```bash
# Enable UFW
sudo ufw enable

# Allow SSH, HTTP, HTTPS
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp

# Block Redis port from external access
sudo ufw deny 6379/tcp

# Check status
sudo ufw status verbose
```

#### Docker and Host Firewall Integration

Docker programs iptables directly. Keep it enabled and enforce host policy using the DOCKER-USER chain (applies before Docker's rules):

```bash
# Block external access to Redis on the host (no effect inside the bridge)
sudo iptables -I DOCKER-USER -p tcp --dport 6379 -j DROP
# Allow from specific trusted subnets if needed:
# sudo iptables -I DOCKER-USER -s 10.0.0.0/8 -p tcp --dport 6379 -j ACCEPT

# Persist rules with your distro's mechanisms (e.g., netfilter-persistent)
```

If you prefer UFW, add rules in `/etc/ufw/before.rules` to filter before Docker's chains, or rely on a cloud firewall (recommended) plus not publishing Redis ports.

### 4. Verification Steps

#### Test Redis Security

1. **From the Droplet itself:**
   ```bash
   # Should connect successfully
   redis-cli ping
   # Expected output: PONG
   ```

2. **From external network:**
   ```bash
   # Should fail to connect
   telnet YOUR_DROPLET_IP 6379
   # Should timeout or be refused
   ```

3. **From Docker containers:**
   ```bash
   # Should connect successfully within Docker network
   docker compose exec -T redis redis-cli ping
   # Expected output: PONG
   ```

### 5. Additional Security Recommendations

1. **Enable Redis Authentication** (Optional but recommended):
   ```bash
   # Generate a secure password
   openssl rand -base64 32
   
   # Add to Redis configuration
   requirepass YOUR_SECURE_PASSWORD
   ```

2. **Regular Security Audits**:
   - Run periodic network scans
   - Monitor Redis access logs
   - Review firewall rules quarterly

3. **Backup Security**:
   - Ensure Redis data backups are encrypted
   - Store backups in secure locations
   - Test restore procedures regularly

### 6. Emergency Response

If Redis security is compromised:

1. **Immediate Actions:**
   - Block port 6379 in firewall
   - Restart Redis with secure configuration
   - Check Redis data for unauthorized access

2. **Assessment:**
   - Review access logs
   - Check for data exfiltration
   - Update all related passwords/keys

3. **Recovery:**
   - Restore from clean backup if necessary
   - Implement additional monitoring
   - Document lessons learned

## Testing Your Configuration

After implementing these changes, verify security by:

1. Running `telnet YOUR_IP 6379` from external network (should fail)
2. Ensuring your application can still connect to Redis
3. Checking that Docker containers can communicate properly
4. Verifying firewall rules are active

## Support

For questions about Redis security configuration, refer to:
- [DigitalOcean Redis Security Guide](https://www.digitalocean.com/community/tutorials/how-to-install-and-secure-redis-on-ubuntu-20-04)
- [Docker Redis Security Best Practices](https://docs.docker.com/engine/security/)
- NeuraTrade documentation in `/docs/` directory