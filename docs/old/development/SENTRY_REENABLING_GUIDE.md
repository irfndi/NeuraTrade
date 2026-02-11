# Sentry Re-enabling Guide

## Overview

This document provides instructions for re-enabling Sentry monitoring in the CCXT service once the Bun.serve instrumentation issue is resolved.

## Current Status

**Status**: Disabled
**Reason**: Bun.serve instrumentation compatibility issue
**Location**: `ccxt-service/sentry.ts:30`
**Date Disabled**: [To be determined from git history]

## Issue Description

The Sentry integration was disabled due to a known compatibility issue between Sentry's instrumentation and Bun's `Bun.serve` API. The instrumentation causes conflicts or errors when both are used together.

## Prerequisites for Re-enabling

Before re-enabling Sentry, ensure the following conditions are met:

1. **Bun Version Check**: Verify you're using Bun 1.3.3 or higher
   ```bash
   bun --version
   ```

2. **Sentry SDK Version**: Check for updates to `@sentry/bun` package
   ```bash
   cd ccxt-service
   bun outdated | grep sentry
   ```

3. **Compatibility Verification**: Check Sentry's official documentation or GitHub issues for Bun.serve compatibility updates
   - [Sentry Bun SDK Documentation](https://docs.sentry.io/platforms/javascript/guides/bun/)
   - [Sentry GitHub Issues](https://github.com/getsentry/sentry-javascript/issues)

## Re-enabling Steps

### Step 1: Update Dependencies

Update Sentry packages to the latest version:

```bash
cd ccxt-service
bun update @sentry/bun @sentry/node
```

### Step 2: Review Configuration

Check the Sentry configuration in `ccxt-service/sentry.ts`:

```typescript
import * as Sentry from '@sentry/bun';

export function initSentry() {
  Sentry.init({
    dsn: process.env.SENTRY_DSN,
    environment: process.env.ENVIRONMENT || 'development',
    tracesSampleRate: process.env.SENTRY_TRACES_SAMPLE_RATE
      ? parseFloat(process.env.SENTRY_TRACES_SAMPLE_RATE)
      : 0.1,
    integrations: [
      // Add Bun-specific integrations if available
    ],
  });
}
```

### Step 3: Uncomment Sentry Initialization

In `ccxt-service/sentry.ts` (line 30 or nearby):

```typescript
// Before (commented out):
// TODO: Re-enable Sentry once the Bun.serve instrumentation issue is resolved.
// initSentry();

// After (uncommented):
initSentry();
```

### Step 4: Add Environment Variables

Ensure the following environment variables are set:

**.env**:
```bash
# Sentry Configuration
SENTRY_DSN=https://your-sentry-dsn@sentry.io/project-id
SENTRY_TRACES_SAMPLE_RATE=0.1
SENTRY_ENVIRONMENT=production
```

**docker-compose.yml** or **Coolify Environment Variables**:
```yaml
environment:
  - SENTRY_DSN=${SENTRY_DSN}
  - SENTRY_TRACES_SAMPLE_RATE=0.1
  - SENTRY_ENVIRONMENT=production
```

### Step 5: Test Locally

1. Start the CCXT service locally:
   ```bash
   cd ccxt-service
   bun run dev
   ```

2. Trigger a test error to verify Sentry is capturing:
   ```bash
   curl http://localhost:3001/test-error
   ```

3. Check Sentry dashboard for the error event

### Step 6: Test in Staging

1. Deploy to staging environment
2. Monitor Sentry dashboard for incoming events
3. Verify no instrumentation conflicts in logs
4. Check application stability over 24-48 hours

### Step 7: Deploy to Production

Once verified in staging:

1. Deploy to production
2. Monitor error rates and performance
3. Set up alerts for Sentry errors

## Testing Sentry Integration

### Manual Testing

Create a test endpoint to verify Sentry is working:

```typescript
// In ccxt-service/index.ts or routes file
app.get('/test-sentry', () => {
  throw new Error('Test error for Sentry');
});
```

### Automated Testing

Add a test to verify Sentry initialization:

```typescript
import { describe, test, expect } from 'bun:test';
import * as Sentry from '@sentry/bun';

describe('Sentry Integration', () => {
  test('should initialize without errors', () => {
    expect(() => {
      Sentry.init({
        dsn: 'test-dsn',
        environment: 'test',
      });
    }).not.toThrow();
  });
});
```

## Rollback Procedure

If issues arise after re-enabling:

1. **Immediate Rollback**: Comment out the Sentry initialization
   ```typescript
   // initSentry();
   ```

2. **Redeploy**: Deploy the change immediately
   ```bash
   make deploy
   ```

3. **Document Issues**: Create a GitHub issue with:
   - Bun version
   - Sentry SDK version
   - Error logs
   - Steps to reproduce

## Monitoring After Re-enabling

### Key Metrics to Monitor

1. **Error Rate**: Check for increased errors in Sentry dashboard
2. **Performance**: Monitor application response times
3. **Memory Usage**: Check for memory leaks
4. **CPU Usage**: Verify no unusual CPU spikes

### Alerting

Set up Sentry alerts for:
- Error rate exceeds threshold (e.g., >10 errors/minute)
- New error types
- Performance degradation (>500ms response times)

## Troubleshooting

### Issue: Sentry Events Not Appearing

**Solution**:
1. Verify SENTRY_DSN is correctly set
2. Check network connectivity to Sentry
3. Review Sentry SDK logs

### Issue: High Memory Usage

**Solution**:
1. Reduce `tracesSampleRate` (e.g., from 0.1 to 0.01)
2. Disable unnecessary integrations
3. Check for memory leaks in Sentry SDK

### Issue: Instrumentation Conflicts

**Solution**:
1. Disable specific Sentry integrations
2. Check Bun and Sentry versions for compatibility
3. Report issue to Sentry GitHub repository

## Additional Resources

- [Sentry Bun Documentation](https://docs.sentry.io/platforms/javascript/guides/bun/)
- [Bun Documentation](https://bun.sh/docs)
- [Sentry Performance Monitoring](https://docs.sentry.io/product/performance/)
- [Sentry Error Tracking](https://docs.sentry.io/product/issues/)

## Support

For issues or questions:
1. Check Sentry documentation
2. Search Sentry GitHub issues
3. Contact Sentry support
4. Consult with DevOps team

---

**Last Updated**: 2025-12-14
**Document Owner**: DevOps Team
**Review Frequency**: Quarterly or when Bun/Sentry versions update
