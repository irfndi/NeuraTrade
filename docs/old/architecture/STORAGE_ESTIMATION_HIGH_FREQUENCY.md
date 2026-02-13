# High-Frequency Data Collection - Storage Estimation

## Current vs Optimized Collection Rates

### Current Configuration
- **Market Data**: 5 minutes (300s) → **30 seconds** (10x increase)
- **Funding Rates**: 5 minutes (300s) → **60 seconds** (5x increase)
- **Orderbook**: Not collected separately → **15 seconds** (new)
- **Ticker**: Not collected separately → **10 seconds** (new)
- **OHLCV**: Not collected separately → **60 seconds** (new)

### Rate Limit Improvements
- **Nginx API limit**: 10 req/s → **100 req/s** (10x increase)
- **Burst capacity**: 20 → **100 requests** (5x increase)
- **Exchange-specific limits**: Optimized per exchange capabilities

## Storage Impact Analysis

### Current Data (from production)
- **Market Data**: 158,972 records = 16 MB
- **Funding Rates**: 3,400 records = 1.4 MB
- **Trading Pairs**: 5,386 records = 0.8 MB
- **Total**: ~18.2 MB

### Average Row Sizes
- **Market Data**: 103.22 bytes/row
- **Funding Rates**: 431.28 bytes/row

## High-Frequency Storage Projections

### Conservative Estimation (2x buffer)
Based on 5x-10x collection rate increase:

#### Daily Growth
- **Market Data**: 158,972 × 10 = 1,589,720 records/day
- **Funding Rates**: 3,400 × 5 = 17,000 records/day
- **Orderbook**: 500,000 records/day (new)
- **Ticker**: 1,000,000 records/day (new)
- **OHLCV**: 200,000 records/day (new)

#### Storage Requirements
- **Market Data**: 1,589,720 × 103.22 bytes = 164.1 MB/day
- **Funding Rates**: 17,000 × 431.28 bytes = 7.3 MB/day
- **Orderbook**: 500,000 × 200 bytes = 95.2 MB/day
- **Ticker**: 1,000,000 × 80 bytes = 76.3 MB/day
- **OHLCV**: 200,000 × 150 bytes = 28.6 MB/day

**Total Daily**: ~471.5 MB/day

### Moderate Estimation (1.5x buffer)
Accounting for additional exchanges and symbols:

#### Daily Growth
- **Total Records**: ~3,500,000 records/day
- **Average Size**: ~150 bytes/record
- **Daily Storage**: ~525 MB/day

### High Activity Estimation (Double buffer)
With maximum collection across all exchanges:

#### Daily Growth
- **Total Records**: ~7,000,000 records/day
- **Average Size**: ~180 bytes/record
- **Daily Storage**: ~1.26 GB/day

### Monthly Projections

| Scenario | Daily | Monthly (30 days) | Quarterly | Annual |
|----------|--------|------------------|-----------|--------|
| Conservative | 471 MB | 14.1 GB | 42.3 GB | 169 GB |
| Moderate | 525 MB | 15.8 GB | 47.3 GB | 189 GB |
| High Activity | 1.26 GB | 37.8 GB | 113.4 GB | 454 GB |
| **Double Buffer** | **2.52 GB** | **75.6 GB** | **226.8 GB** | **908 GB** |

## Database Optimization Recommendations

### Partitioning Strategy
```sql
-- Partition by date for efficient cleanup
CREATE TABLE market_data_2024_12 PARTITION OF market_data
FOR VALUES FROM ('2024-12-01') TO ('2025-01-01');

-- Auto-partitioning script
CREATE OR REPLACE FUNCTION create_monthly_partition()
RETURNS void AS $$
BEGIN
    EXECUTE format('CREATE TABLE IF NOT EXISTS market_data_%s PARTITION OF market_data FOR VALUES FROM (%L) TO (%L)', 
                   to_char(CURRENT_DATE, 'YYYY_MM'),
                   date_trunc('month', CURRENT_DATE),
                   date_trunc('month', CURRENT_DATE + interval '1 month'));
END;
$$ LANGUAGE plpgsql;
```

### Indexing Optimization
```sql
-- Composite indexes for faster queries
CREATE INDEX idx_market_data_exchange_symbol_time ON market_data(exchange_id, trading_pair_id, detected_at DESC);
CREATE INDEX idx_funding_rates_time ON funding_rates(detected_at DESC);
CREATE INDEX idx_orderbook_exchange_symbol_time ON orderbook_data(exchange_id, trading_pair_id, detected_at DESC);
```

### Data Retention Policies
```sql
-- Automatic cleanup for old data
-- Keep 90 days of high-frequency data
DELETE FROM market_data WHERE detected_at < NOW() - INTERVAL '90 days';
DELETE FROM orderbook_data WHERE detected_at < NOW() - INTERVAL '7 days';
DELETE FROM ticker_data WHERE detected_at < NOW() - INTERVAL '30 days';
```

## Infrastructure Recommendations

### Database Server Requirements
- **RAM**: 16-32 GB (for caching and connections)
- **Storage**: 1TB NVMe SSD (for high IOPS)
- **CPU**: 8-16 cores (for parallel processing)
- **Network**: 1 Gbps minimum

### Connection Pooling
```yaml
# docker-compose.yml optimization
services:
  postgres:
    environment:
      - POSTGRES_MAX_CONNECTIONS=200
      - POSTGRES_SHARED_BUFFERS=4GB
      - POSTGRES_EFFECTIVE_CACHE_SIZE=12GB
      - POSTGRES_MAINTENANCE_WORK_MEM=1GB
      - POSTGRES_CHECKPOINT_COMPLETION_TARGET=0.9
      - POSTGRES_WAL_BUFFERS=16MB
      - POSTGRES_DEFAULT_STATISTICS_TARGET=100
```

### Monitoring Queries
```sql
-- Monitor table sizes
SELECT 
    schemaname,
    tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) as size
FROM pg_tables 
WHERE schemaname NOT IN ('information_schema', 'pg_catalog')
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;

-- Monitor growth rate
SELECT 
    table_name,
    (n_live_tup * 100 / (CASE WHEN n_dead_tup = 0 THEN 1 ELSE n_dead_tup END))::int as bloat_ratio
FROM pg_stat_user_tables
WHERE n_live_tup > 1000;
```

## Implementation Steps

1. **Deploy Optimized Configuration**
   ```bash
   chmod +x scripts/optimize-rate-limits.sh
   ./scripts/optimize-rate-limits.sh
   ```

2. **Monitor Initial Impact**
   - Track collection rates for 24-48 hours
   - Monitor database growth
   - Check for rate limit violations

3. **Scale Infrastructure**
   - Upgrade database server if needed
   - Implement partitioning
   - Set up automated cleanup

4. **Fine-tune Parameters**
   - Adjust collection intervals based on actual usage
   - Optimize batch sizes
   - Balance collection vs storage costs

## Risk Mitigation

- **Rate Limit Monitoring**: Implement alerts when approaching limits
- **Storage Monitoring**: Daily checks on database growth
- **Automatic Cleanup**: Ensure old data is removed regularly
- **Backup Strategy**: Daily backups with 30-day retention
- **Rollback Plan**: Quick rollback to previous configuration if needed

## Cost Analysis

### Monthly Infrastructure Costs
- **Database Server**: $200-500/month (depending on provider)
- **Storage**: $50-200/month (1TB NVMe)
- **Backup Storage**: $20-50/month
- **Monitoring**: $10-30/month

**Total**: $280-780/month for high-frequency collection

### ROI Considerations
- **More Opportunities**: 5-10x more arbitrage opportunities detected
- **Faster Detection**: Real-time vs 5-minute delay
- **Better Accuracy**: More data points for analysis
- **Competitive Advantage**: Earlier detection of market inefficiencies